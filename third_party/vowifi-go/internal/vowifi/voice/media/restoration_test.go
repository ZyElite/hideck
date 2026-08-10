package media

import (
	"encoding/binary"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

const rtcpSilenceProbeTimeout = 100 * time.Millisecond

func TestOriginalAddressAndPTMappingForms(t *testing.T) {
	relay := NewRTPRelay(listenMediaUDP(t), listenMediaUDP(t))
	if err := relay.SetRemoteAddr("127.0.0.1", 23000); err != nil {
		t.Fatal(err)
	}
	if err := relay.SetClientAddr("127.0.0.1", 24000); err != nil {
		t.Fatal(err)
	}
	if relay.remoteAddr.Load().Port != 23000 || relay.remoteAddrRTCP.Load().Port != 23001 {
		t.Fatalf("IMS address pair = %v/%v", relay.remoteAddr.Load(), relay.remoteAddrRTCP.Load())
	}
	if relay.clientAddr.Load().Port != 24000 || relay.clientAddrRTCP.Load().Port != 24001 {
		t.Fatalf("client address pair = %v/%v", relay.clientAddr.Load(), relay.clientAddrRTCP.Load())
	}
	relay.SetPTMapping(96, 8)
	imsPacket := []byte{0x80, 96}
	lanPacket := []byte{0x80, 8}
	relay.applyIMSPayloadTypeMapping(imsPacket)
	relay.applyLANPayloadTypeMapping(lanPacket)
	if imsPacket[1] != 8 || lanPacket[1] != 96 {
		t.Fatalf("PT pair mapping IMS=%d LAN=%d", imsPacket[1], lanPacket[1])
	}
}

func TestRTPRelayForwardsRTCPBothDirections(t *testing.T) {
	imsRTP := listenMediaUDP(t)
	lanRTP := listenMediaUDP(t)
	imsRTCP := listenMediaUDP(t)
	lanRTCP := listenMediaUDP(t)
	imsPeerRTP, imsPeerRTCP := listenConsecutivePair(t)
	lanPeerRTP, lanPeerRTCP := listenConsecutivePair(t)
	relay := newRTPRelay(imsRTP, lanRTP, imsRTCP, lanRTCP)
	relay.connLAN = lanRTP
	if err := relay.SetRemoteAddr(imsPeerRTP.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	if err := relay.SetClientAddr(lanPeerRTP.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	if err := relay.StartCurrent(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(relay.Stop)

	packet := []byte{0x80, 0xc9, 0, 1, 1, 2, 3, 4}
	writeUDPTo(t, imsPeerRTCP, imsRTCP.LocalAddr().(*net.UDPAddr), packet)
	if got := readUDP(t, lanPeerRTCP); string(got) != string(packet) {
		t.Fatalf("IMS RTCP forwarded packet = %x", got)
	}
	writeUDPTo(t, lanPeerRTCP, lanRTCP.LocalAddr().(*net.UDPAddr), packet)
	if got := readUDP(t, imsPeerRTCP); string(got) != string(packet) {
		t.Fatalf("LAN RTCP forwarded packet = %x", got)
	}
	_, _, imsRTCPBytes, lanRTCPBytes := relay.Stats()
	if imsRTCPBytes != uint64(len(packet)) || lanRTCPBytes != uint64(len(packet)) {
		t.Fatalf("RTCP stats = %d/%d", imsRTCPBytes, lanRTCPBytes)
	}
	if atomic.LoadUint32(&relay.imsRTCPFirstPacket) != 1 || atomic.LoadUint32(&relay.lanRTCPFirstPacket) != 1 {
		t.Fatal("successful RTCP forwarding did not record first-packet state")
	}
	oversized := make([]byte, mediaReadBufferSize+256)
	writeUDPTo(t, imsPeerRTCP, imsRTCP.LocalAddr().(*net.UDPAddr), oversized)
	if got := readUDP(t, lanPeerRTCP); len(got) != mediaReadBufferSize {
		t.Fatalf("oversized RTCP forwarded bytes = %d, want %d", len(got), mediaReadBufferSize)
	}
}

func TestRTCPActivityCountsWithoutForwardingDestination(t *testing.T) {
	relay := newRTPRelay(nil, nil, nil, nil)
	relay.Monitor = NewRTPMonitor()
	packet := []byte{0x80, 0xc9, 0, 1, 1, 2, 3, 4}
	before := time.Now().UnixNano()
	relay.handleIMSRTCPPacket(packet, &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 5001})
	relay.handleLANRTCPPacket(packet, &net.UDPAddr{IP: net.IPv4(198, 51, 100, 1), Port: 6001})

	_, _, imsBytes, lanBytes := relay.Stats()
	if imsBytes != uint64(len(packet)) || lanBytes != uint64(len(packet)) {
		t.Fatalf("RTCP received bytes = %d/%d", imsBytes, lanBytes)
	}
	imsCount, lanCount := relay.Monitor.Counts()
	if imsCount != 1 || lanCount != 1 {
		t.Fatalf("RTCP monitor counts = %d/%d", imsCount, lanCount)
	}
	if relay.Monitor.LastIMSToLAN.Load() < before || relay.Monitor.LastLANToIMS.Load() < before {
		t.Fatal("RTCP did not update media activity timestamps")
	}
	if relay.clientAddrRTCP.Load() != nil {
		t.Fatal("LAN RTCP incorrectly replaced the SDP-configured client address")
	}
}

func TestIMSRTCPUsesConfiguredDestinationWithoutSourceFiltering(t *testing.T) {
	lanRTCP := listenMediaUDP(t)
	clientPeer := listenMediaUDP(t)
	relay := newRTPRelay(nil, nil, nil, lanRTCP)
	relay.clientAddrRTCP.Store(clientPeer.LocalAddr().(*net.UDPAddr))
	relay.remoteAddrRTCP.Store(&net.UDPAddr{IP: net.IPv4(203, 0, 113, 9), Port: 9001})
	packet := []byte{0x80, 0xc9, 0, 1, 8, 7, 6, 5}
	relay.handleIMSRTCPPacket(packet, &net.UDPAddr{IP: net.IPv4(192, 0, 2, 9), Port: 7001})
	if got := readUDP(t, clientPeer); string(got) != string(packet) {
		t.Fatalf("forwarded RTCP = %x, want %x", got, packet)
	}
}

func TestRTCPKeepaliveWireSuppressionAndTimerReplacement(t *testing.T) {
	imsRTCP := listenMediaUDP(t)
	imsPeer := listenMediaUDP(t)
	relay := newRTPRelay(nil, nil, imsRTCP, nil)
	relay.remoteAddrRTCP.Store(imsPeer.LocalAddr().(*net.UDPAddr))
	relay.mu.Lock()
	relay.active = true
	relay.mu.Unlock()

	relay.startRTCPKeepaliveLoop()
	relay.rtcpMu.Lock()
	firstTimer := relay.rtcpKeepaliveTimer
	relay.rtcpMu.Unlock()
	relay.startRTCPKeepaliveLoop()
	if firstTimer.Stop() {
		t.Fatal("replaced RTCP keepalive timer remained active")
	}
	relay.runRTCPKeepalive()
	if got := readUDP(t, imsPeer); string(got) != string(emptyReceiverReport) {
		t.Fatalf("RTCP keepalive = %x, want %x", got, emptyReceiverReport)
	}
	relay.Stop()

	silentRTCP := listenMediaUDP(t)
	silentPeer := listenMediaUDP(t)
	silent := newRTPRelay(nil, nil, silentRTCP, nil)
	silent.remoteAddrRTCP.Store(silentPeer.LocalAddr().(*net.UDPAddr))
	silent.Monitor = NewRTPMonitor()
	silent.Monitor.UpdateLAN()
	silent.mu.Lock()
	silent.active = true
	silent.mu.Unlock()
	silent.runRTCPKeepalive()
	if err := silentPeer.SetReadDeadline(time.Now().Add(rtcpSilenceProbeTimeout)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, len(emptyReceiverReport))
	if _, _, err := silentPeer.ReadFromUDP(buffer); !isRTPRelayReadTimeout(err) {
		t.Fatalf("recent outbound activity did not suppress RTCP keepalive: %v", err)
	}
	silent.Stop()
}

func TestRTPRelayWritesOriginalPCAPWireFormat(t *testing.T) {
	imsRelay := listenMediaUDP(t)
	lanRelay := listenMediaUDP(t)
	imsPeer := listenMediaUDP(t)
	lanPeer := listenMediaUDP(t)
	relay := NewRTPRelay(imsRelay, lanRelay)
	relay.SetLogContext("device/30", "trace-30")
	if err := relay.SetRemoteAddr(imsPeer.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	if err := relay.SetClientAddr(lanPeer.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := relay.StartPCAP(directory); err != nil {
		t.Fatal(err)
	}
	if err := relay.StartCurrent(); err != nil {
		t.Fatal(err)
	}
	writeMediaRTP(t, lanPeer, relay.LANPort(), 8)
	_ = readMediaRTP(t, imsPeer)
	if err := relay.StopCurrent(); err != nil {
		t.Fatal(err)
	}

	files, err := filepath.Glob(filepath.Join(directory, "*.pcap"))
	if err != nil || len(files) != 1 {
		t.Fatalf("capture files=%v err=%v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 24+16+1+12 {
		t.Fatalf("short PCAP: %d", len(data))
	}
	if got := binary.LittleEndian.Uint32(data[20:24]); got != pcapLinkUser0 {
		t.Fatalf("PCAP link type=%d want %d", got, pcapLinkUser0)
	}
	if got := binary.LittleEndian.Uint32(data[32:36]); got != 13 {
		t.Fatalf("captured length=%d want 13", got)
	}
	if data[40] != pcapDirectionLANToIMS {
		t.Fatalf("direction=%d want LAN-to-IMS", data[40])
	}
}

func TestRTPRelayReportsPCAPWriteFailure(t *testing.T) {
	relay := NewRTPRelay(nil, nil)
	writer := &failingCaptureWriter{}
	if err := relay.StartPCAP(writer); err != nil {
		t.Fatal(err)
	}
	relay.writePCAPPacket([]byte{1, 2, 3}, pcapDirectionIMSToLAN)
	if err := relay.StopPCAP(); err == nil {
		t.Fatal("expected persisted PCAP write failure")
	}
}

func TestRTPRelaySendsRFC4733EndPackets(t *testing.T) {
	imsRelay := listenMediaUDP(t)
	imsPeer := listenMediaUDP(t)
	relay := NewRTPRelay(imsRelay, nil)
	if err := relay.SetRemoteAddr(imsPeer.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	if err := relay.SetDTMFPayloadType(101); err != nil {
		t.Fatal(err)
	}
	if err := relay.SendDTMF('2', 40*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	packets := make([][]byte, 5)
	for index := range packets {
		packets[index] = readUDP(t, imsPeer)
	}
	timestamp := binary.BigEndian.Uint32(packets[0][4:8])
	for index, packet := range packets {
		if len(packet) != 16 || packet[1]&0x7f != 101 || packet[12] != 2 {
			t.Fatalf("packet %d = %x", index, packet)
		}
		if binary.BigEndian.Uint32(packet[4:8]) != timestamp {
			t.Fatalf("packet %d changed RTP timestamp", index)
		}
	}
	if packets[0][1]&0x80 == 0 || packets[1][1]&0x80 != 0 {
		t.Fatal("RFC4733 marker must be set only on the first packet")
	}
	for index := 2; index < 5; index++ {
		if packets[index][13]&0x80 == 0 {
			t.Fatalf("packet %d missing RFC4733 end bit", index)
		}
	}
	if got := binary.BigEndian.Uint16(packets[4][14:16]); got != 320 {
		t.Fatalf("final event duration=%d want 320", got)
	}
}

func TestRTPRelayRejectsDTMFWithoutNegotiatedPayload(t *testing.T) {
	relay := NewRTPRelay(listenMediaUDP(t), nil)
	if err := relay.SetRemoteAddr(listenMediaUDP(t).LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	if err := relay.SendDTMF('2', 40*time.Millisecond); err == nil {
		t.Fatal("expected missing telephone-event negotiation error")
	}
}

func listenConsecutivePair(t *testing.T) (*net.UDPConn, *net.UDPConn) {
	t.Helper()
	for attempt := 0; attempt < 100; attempt++ {
		first := listenMediaUDP(t)
		port := first.LocalAddr().(*net.UDPAddr).Port
		second, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port + 1})
		if err == nil {
			t.Cleanup(func() { _ = second.Close() })
			return first, second
		}
		_ = first.Close()
	}
	t.Fatal("unable to allocate consecutive UDP ports")
	return nil, nil
}

func writeUDPTo(t *testing.T, conn *net.UDPConn, address *net.UDPAddr, packet []byte) {
	t.Helper()
	if _, err := conn.WriteToUDP(packet, address); err != nil {
		t.Fatal(err)
	}
}

func readUDP(t *testing.T, conn *net.UDPConn) []byte {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 2048)
	n, _, err := conn.ReadFromUDP(buffer)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), buffer[:n]...)
}

type failingCaptureWriter struct {
	writes int
}

func (w *failingCaptureWriter) Write(data []byte) (int, error) {
	w.writes++
	if w.writes == 1 {
		return len(data), nil
	}
	return 0, errors.New("forced PCAP write failure")
}

func (*failingCaptureWriter) Close() error { return nil }
