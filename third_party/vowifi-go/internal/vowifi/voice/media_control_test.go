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
	configureRelayDTMF(relay, answer)
	agent := NewAgent("device-30", nil, nil)
	call := NewCall(agent, callstate.DirectionOutbound, "call-30", "43430")
	call.SetRTPRelay(relay)
	agent.mu.Lock()
	agent.calls[call.CallID()] = call
	agent.activeCall = call
	agent.mu.Unlock()
	captureDirectory := t.TempDir()
	if err := agent.StartPCAP(captureDirectory); err != nil {
		t.Fatal(err)
	}
	if err := agent.SendDTMF("call-30", "2"); err != nil {
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
	if err := agent.SendDTMF("missing-call", "2"); err == nil {
		t.Fatal("expected missing call registry error")
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
