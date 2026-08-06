package media

import (
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
	if err := relay.Start(); err != nil {
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
	// Read one comfort noise packet.
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read comfort noise: %v", err)
	}
	if n != 13 {
		t.Errorf("comfort noise len = %d, want 13", n)
	}
	if buf[1] != 13 {
		t.Errorf("payload type = %d, want 13 (CN)", buf[1])
	}
	g.Stop()
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

func TestMediaSessionManager(t *testing.T) {
	m := NewMediaSessionManager()
	r, err := m.CreateRelay("call-1", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("CreateRelay: %v", err)
	}
	if m.GetRelay("call-1") != r {
		t.Error("GetRelay mismatch")
	}
	if err := m.Release("call-1"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if m.GetRelay("call-1") != nil {
		t.Error("relay should be released")
	}
}
