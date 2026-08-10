package media

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestRTPRelayLifecycle(t *testing.T) {
	imsConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen ims: %v", err)
	}
	lanConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen lan: %v", err)
	}
	relay := NewRTPRelay(imsConn, lanConn)
	relay.SetRemoteAddr(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: imsConn.LocalAddr().(*net.UDPAddr).Port})
	relay.SetClientAddr(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: lanConn.LocalAddr().(*net.UDPAddr).Port})
	if err := relay.StartCurrent(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer relay.Stop()

	if relay.IMSPort() == 0 || relay.LANPort() == 0 {
		t.Error("ports should be non-zero")
	}
	// Send a packet IMS->LAN and verify it arrives.
	imsConn.WriteTo([]byte{0x80, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0, 0, 0}, relay.imsRemote)
	buf := make([]byte, 64)
	lanConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := lanConn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read relayed packet: %v", err)
	}
	if n != 12 {
		t.Errorf("relayed packet len = %d, want 12", n)
	}
}

func TestRTPMonitorOneWay(t *testing.T) {
	m := NewRTPMonitor()
	m.UpdateIMS()
	m.UpdateLAN()
	if m.OneWay(100 * time.Millisecond) {
		t.Error("should not be one-way with both sides active")
	}
	// Stop LAN updates; after timeout, one-way should trigger. The first
	// check arms the detector; keep IMS active while the timeout elapses,
	// then confirm.
	time.Sleep(150 * time.Millisecond)
	m.UpdateIMS()
	_ = m.OneWay(100 * time.Millisecond)
	time.Sleep(120 * time.Millisecond)
	m.UpdateIMS() // keep IMS side active
	if !m.OneWay(100 * time.Millisecond) {
		t.Error("should be one-way after LAN silence")
	}
}

func TestComfortNoiseGenerator(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer conn.Close()
	g := NewComfortNoiseGenerator()
	if err := g.Start(conn, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: conn.LocalAddr().(*net.UDPAddr).Port}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer g.Stop()
	buf := make([]byte, 256)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read comfort noise: %v", err)
	}
	if n != 172 {
		t.Fatalf("comfort noise len = %d, want 172", n)
	}
	if buf[1]&0x7f != 0 {
		t.Errorf("payload type = %d, want 0 (PCMU)", buf[1]&0x7f)
	}
	firstSequence := binary.BigEndian.Uint16(buf[2:4])
	firstTimestamp := binary.BigEndian.Uint32(buf[4:8])
	if got := binary.BigEndian.Uint32(buf[8:12]); got != comfortNoiseSSRC {
		t.Errorf("SSRC = 0x%x, want 0x%x", got, comfortNoiseSSRC)
	}
	n, _, err = conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read second comfort noise packet: %v", err)
	}
	if got := binary.BigEndian.Uint16(buf[2:4]); got != firstSequence+1 {
		t.Errorf("sequence = %d, want %d", got, firstSequence+1)
	}
	if got := binary.BigEndian.Uint32(buf[4:8]); got != firstTimestamp+comfortNoiseSamples {
		t.Errorf("timestamp = %d, want %d", got, firstTimestamp+comfortNoiseSamples)
	}
}

func TestComfortNoiseGeneratorReportsWriteFailure(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	remote := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: conn.LocalAddr().(*net.UDPAddr).Port}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	generator := NewComfortNoiseGenerator()
	if err := generator.Start(conn, remote); err != nil {
		t.Fatal(err)
	}
	defer generator.Stop()
	select {
	case err := <-generator.Errors():
		if err == nil {
			t.Fatal("expected explicit RTP write error")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for RTP write error")
	}
	if err := generator.Start(conn, remote); err == nil {
		t.Fatal("failed comfort-noise generator reported a successful restart")
	}
}

func TestLinearToUlaw(t *testing.T) {
	// Zero sample -> u-law 0xFF (silence).
	if got := linearToUlaw(0); got != 0xFF {
		t.Errorf("linearToUlaw(0) = 0x%02X, want 0xFF", got)
	}
	// Positive sample: u-law complements the sign bit, so the encoded byte
	// has bit 7 set (standard G.711 convention).
	if got := linearToUlaw(1000); got&0x80 == 0 {
		t.Errorf("linearToUlaw(1000) sign bit not set: 0x%02X", got)
	}
	// Negative sample: sign bit clear after complement.
	if got := linearToUlaw(-1000); got&0x80 != 0 {
		t.Errorf("linearToUlaw(-1000) sign bit set: 0x%02X", got)
	}
}

func TestPTMapping(t *testing.T) {
	imsConn, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	lanConn, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	defer imsConn.Close()
	defer lanConn.Close()
	relay := NewRTPRelay(imsConn, lanConn)
	relay.SetPTMapping(map[int]int{8: 96}) // LAN PT 8 -> IMS PT 96

	pkt := []byte{0x80, 0x08, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	relay.applyLANPayloadTypeMapping(pkt)
	if pkt[1] != 96 {
		t.Errorf("LAN->IMS PT = %d, want 96", pkt[1])
	}
	pkt2 := []byte{0x80, 0x60, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0} // PT 96
	relay.applyIMSPayloadTypeMapping(pkt2)
	if pkt2[1] != 8 {
		t.Errorf("IMS->LAN PT = %d, want 8", pkt2[1])
	}
}

func TestRTPRelayAppliesPTMappingOnNetworkPath(t *testing.T) {
	imsRelay := listenMediaUDP(t)
	lanRelay := listenMediaUDP(t)
	imsPeer := listenMediaUDP(t)
	lanPeer := listenMediaUDP(t)
	relay := NewRTPRelay(imsRelay, lanRelay)
	relay.SetRemoteAddr(imsPeer.LocalAddr().(*net.UDPAddr))
	relay.SetClientAddr(lanPeer.LocalAddr().(*net.UDPAddr))
	relay.SetPTMapping(map[int]int{8: 96})
	if err := relay.StartCurrent(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(relay.Stop)

	writeMediaRTP(t, lanPeer, relay.LANPort(), 8)
	if got := readMediaRTP(t, imsPeer); got != 96 {
		t.Fatalf("LAN->IMS payload type = %d, want 96", got)
	}
	writeMediaRTP(t, imsPeer, relay.IMSPort(), 96)
	if got := readMediaRTP(t, lanPeer); got != 8 {
		t.Fatalf("IMS->LAN payload type = %d, want 8", got)
	}
}

func listenMediaUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func writeMediaRTP(t *testing.T, conn *net.UDPConn, port, payloadType int) {
	t.Helper()
	packet := []byte{0x80, byte(payloadType), 0, 1, 0, 0, 0, 0, 0, 0, 0, 1}
	if _, err := conn.WriteToUDP(packet, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}); err != nil {
		t.Fatal(err)
	}
}

func readMediaRTP(t *testing.T, conn *net.UDPConn) int {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	packet := make([]byte, 64)
	n, _, err := conn.ReadFromUDP(packet)
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Fatalf("short RTP packet: %d bytes", n)
	}
	return int(packet[1] & 0x7f)
}

func TestMediaSessionManager(t *testing.T) {
	m := NewMediaSessionManager()
	r, err := m.CreateRelay("call-1", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("CreateRelay: %v", err)
	}
	if m.GetRelay("call-1") != r {
		t.Error("GetRelay mismatch")
	}
	m.Start()
	if err := m.ReleaseCurrent("call-1"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if m.GetRelay("call-1") != nil {
		t.Error("relay should be released")
	}
}

func TestMediaSessionManagerReplacementStopsPreviousRelay(t *testing.T) {
	m := NewMediaSessionManager()
	first, err := m.CreateRelay("call-1", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.CreateRelay("call-1", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if first == second || m.GetRelay("call-1") != second {
		t.Fatal("replacement relay was not installed")
	}
	if err := first.StartCurrent(); err == nil {
		t.Fatal("replaced relay remained usable")
	}
	if err := m.ReleaseCurrent(); err != nil {
		t.Fatal(err)
	}
	if err := m.StartCurrent(); err == nil {
		t.Fatal("released manager reported a successful start")
	}
}

func TestMediaSessionManagerLegacyReplacementRemovesCallAlias(t *testing.T) {
	m := NewMediaSessionManager()
	additive, err := m.CreateRelay("call-1", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := m.CreateRelay("127.0.0.1", "127.0.0.1", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if m.GetRelay("call-1") != nil || m.GetRelay() != legacy {
		t.Fatal("legacy replacement retained an additive relay alias")
	}
	if err := additive.StartCurrent(); err == nil {
		t.Fatal("legacy replacement left additive relay usable")
	}
	m.Release()
}

func TestComfortNoiseCannotRestartAfterStop(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	remote := conn.LocalAddr().(*net.UDPAddr)
	generator := NewComfortNoiseGenerator(conn, remote, "device", "trace")
	if err := generator.Start(); err != nil {
		t.Fatal(err)
	}
	generator.Stop()
	if err := generator.Start(); err == nil {
		t.Fatal("stopped comfort-noise generator reported a successful restart")
	}
}

func TestBridgeReplacementStopsPreviousRelay(t *testing.T) {
	bridge := NewBridge("device")
	first := NewRTPRelay(nil, nil)
	second := NewRTPRelay(nil, nil)
	if _, err := bridge.SetupRelay(first); err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.SetupRelay(second); err != nil {
		t.Fatal(err)
	}
	if err := first.StartCurrent(); err == nil {
		t.Fatal("replaced bridge relay remained usable")
	}
	second.Stop()
}
