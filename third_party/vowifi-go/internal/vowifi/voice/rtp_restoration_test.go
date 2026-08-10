package voice

import (
	"net"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/media"
)

type originalCallMediaLifecycle interface {
	StartMedia()
	StopMedia()
	StartPCAP(string) error
	StopPCAP()
	IsConnected() bool
	IsTerminalState() bool
	GetStartTime() time.Time
	GetEndTime() time.Time
}

var _ originalCallMediaLifecycle = (*Call)(nil)

func TestCallMediaLifecycleDrivesRealRTPAndRecoveredState(t *testing.T) {
	imsRelay := listenVoiceRTP(t)
	lanRelay := listenVoiceRTP(t)
	imsPeer := listenVoiceRTP(t)
	lanPeer := listenVoiceRTP(t)
	relay := media.NewRTPRelay(imsRelay, lanRelay)
	if err := relay.SetRemoteAddr(imsPeer.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	if err := relay.SetClientAddr(lanPeer.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}

	call := NewCall(nil, callstate.DirectionOutbound, "rtp-lifecycle-31", "43430")
	t.Cleanup(cleanupRestorationCall(call))
	call.SetRTPRelay(relay)
	if err := call.TransitionChecked(callstate.StateCalling); err != nil {
		t.Fatal(err)
	}
	call.startPrackRuntimeRetransmission(time.Hour, func() {})
	startedAfter := time.Now()
	if err := call.StartMediaCurrent(); err != nil {
		t.Fatal(err)
	}
	if !call.IsConnected() || call.GetState() != int(callstate.StateConnected) {
		t.Fatalf("state after StartMediaCurrent = %s", call.CallState())
	}
	if call.GetStartTime().Before(startedAfter) {
		t.Fatalf("start time = %v, before media start %v", call.GetStartTime(), startedAfter)
	}

	packet := []byte{0x80, 0x00, 0x00, 0x01, 0, 0, 0, 1, 0, 0, 0, 2}
	writeVoiceRTP(t, imsPeer, imsRelay.LocalAddr().(*net.UDPAddr), packet)
	if got := readVoiceRTP(t, lanPeer); string(got) != string(packet) {
		t.Fatalf("forwarded RTP = %x, want %x", got, packet)
	}

	stoppedAfter := time.Now()
	call.StopMedia()
	if !call.IsTerminalState() || call.GetState() != int(callstate.StateTerminated) {
		t.Fatalf("state after StopMedia = %s", call.CallState())
	}
	if call.GetEndTime().Before(stoppedAfter) {
		t.Fatalf("end time = %v, before media stop %v", call.GetEndTime(), stoppedAfter)
	}
	call.mu.RLock()
	prackTimer, prackRetry, prackDeadline := call.prackTimer, call.prackRetransmit, call.prackDeadline
	call.mu.RUnlock()
	if prackTimer != nil || prackRetry != nil || !prackDeadline.IsZero() {
		t.Fatal("StopMedia did not clear the PRACK retransmission lifecycle")
	}
	if err := relay.StartCurrent(); err == nil {
		t.Fatal("StopMedia did not close the RTP relay")
	}
}

func listenVoiceRTP(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func writeVoiceRTP(t *testing.T, conn *net.UDPConn, target *net.UDPAddr, packet []byte) {
	t.Helper()
	if _, err := conn.WriteToUDP(packet, target); err != nil {
		t.Fatal(err)
	}
}

func readVoiceRTP(t *testing.T, conn *net.UDPConn) []byte {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 1500)
	n, _, err := conn.ReadFromUDP(buffer)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), buffer[:n]...)
}
