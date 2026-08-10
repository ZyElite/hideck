package voice

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/media"
)

func TestAgentSendDTMFUsesCallRegistryAndNegotiatedPayload(t *testing.T) {
	imsRelay := listenVoiceMediaUDP(t)
	imsPeer := listenVoiceMediaUDP(t)
	relay := media.NewRTPRelay(imsRelay, nil)
	if err := relay.SetRemoteAddr(imsPeer.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	answer, err := ParseSDP([]byte("v=0\r\nc=IN IP4 127.0.0.1\r\nm=audio 25000 RTP/AVP 97\r\na=rtpmap:97 telephone-event/8000\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := configureRelayDTMF(relay, answer); err != nil {
		t.Fatal(err)
	}
	if err := relay.StartCurrent(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(relay.Stop)
	agent := NewAgent("device-30", nil, nil)
	call := NewCall(agent, callstate.DirectionOutbound, "call-30", "43430")
	t.Cleanup(func() { call.Cancel(); call.CloseDone() })
	call.SetClientCallID("client-call-30")
	call.SetRTPRelay(relay)
	call.StartMedia()
	agent.mu.Lock()
	agent.calls[call.CallID()] = call
	agent.activeCall = call
	agent.mu.Unlock()
	gateway := NewGateway(agent)
	captureDirectory := t.TempDir()
	if err := agent.StartPCAP(captureDirectory); err != nil {
		t.Fatal(err)
	}
	if err := gateway.SendDTMF("client-call-30", "2"); err != nil {
		t.Fatal(err)
	}
	if err := agent.StopPCAP(); err != nil {
		t.Fatal(err)
	}
	assertAgentPCAP(t, captureDirectory)
	if err := imsPeer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	packet := make([]byte, 32)
	n, _, err := imsPeer.ReadFromUDP(packet)
	if err != nil {
		t.Fatal(err)
	}
	if n != 16 || packet[1]&0x7f != 97 || packet[12] != 2 {
		t.Fatalf("DTMF packet=%x", packet[:n])
	}
	invalid, err := ParseSDP([]byte("v=0\r\nm=audio 25000 RTP/AVP 98\r\n" +
		"a=rtpmap:98 telephone-event/not-a-rate\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := configureRelayDTMF(relay, invalid); err == nil {
		t.Fatal("expected invalid re-negotiation error")
	}
	if err := gateway.SendDTMF("client-call-30", "2"); err != nil {
		t.Fatalf("failed re-negotiation replaced active DTMF config: %v", err)
	}
	withoutEvents, err := ParseSDP([]byte("v=0\r\nm=audio 25000 RTP/AVP 8\r\na=rtpmap:8 PCMA/8000\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := configureRelayDTMF(relay, withoutEvents); err != nil {
		t.Fatal(err)
	}
	if err := agent.SendDTMF("client-call-30", "2"); err == nil {
		t.Fatal("expected removed telephone-event negotiation error")
	}
	if err := agent.SendDTMF("missing-call", "2"); err == nil {
		t.Fatal("expected missing call registry error")
	}
	registered := NewCall(agent, callstate.DirectionOutbound, "registered-not-active", "43430")
	t.Cleanup(func() { registered.Cancel(); registered.CloseDone() })
	registered.StartMedia()
	agent.mu.Lock()
	agent.calls[registered.CallID()] = registered
	agent.mu.Unlock()
	if err := agent.SendDTMF(registered.CallID(), "2"); err == nil {
		t.Fatal("expected registered but inactive call error")
	}
}

func TestCallSendDTMFRequiresConnectedState(t *testing.T) {
	call := NewCall(nil, callstate.DirectionOutbound, "not-connected", "43430")
	t.Cleanup(func() { call.Cancel(); call.CloseDone() })
	if err := call.SendDTMF("2"); err == nil {
		t.Fatal("expected disconnected call error")
	}
}

func assertAgentPCAP(t *testing.T, directory string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(directory, "*.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("PCAP files = %v, want one", matches)
	}
	info, err := os.Stat(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	const globalHeaderLength = 24
	if info.Size() <= globalHeaderLength {
		t.Fatalf("PCAP size = %d, want captured RTP data", info.Size())
	}
}

func listenVoiceMediaUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}
