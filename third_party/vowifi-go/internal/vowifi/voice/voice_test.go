package voice

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

// newTestAgent builds an agent with a fake IMS service.
func newTestAgent(t *testing.T) *Agent {
	t.Helper()
	registrar := startVoiceTestRegistrar(t)
	cfg := &imscore.IMSConfig{
		DeviceID:    "dev-1",
		IMSI:        "310260123456789",
		IMPI:        "310260123456789@ims.example.com",
		Domain:      "ims.example.com",
		LocalIP:     net.IPv4(127, 0, 0, 1),
		Registrar:   registrar.LocalAddr().String(),
		AKAProvider: stubAKA{},
	}
	svc, err := imscore.New(cfg)
	if err != nil {
		t.Fatalf("imscore.New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.Register(ctx); err != nil {
		t.Fatalf("imscore.Register: %v", err)
	}
	return NewAgent("dev-1", svc, nil)
}

func startVoiceTestRegistrar(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		buffer := make([]byte, 64*1024)
		n, remote, readErr := conn.ReadFromUDP(buffer)
		if readErr != nil {
			return
		}
		request := string(buffer[:n])
		response := fmt.Sprintf("SIP/2.0 200 OK\r\nVia: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
			voiceTestHeader(request, "Via"), voiceTestHeader(request, "Call-ID"), voiceTestHeader(request, "CSeq"))
		_, _ = conn.WriteToUDP([]byte(response), remote)
	}()
	return conn
}

func voiceTestHeader(message, name string) string {
	for _, line := range strings.Split(message, "\r\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(key), name) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// stubAKA is a deterministic AKA provider.
type stubAKA struct{}

func (stubAKA) CalculateAKA(rand16, autn16 []byte) (imscore.AKAResult, error) {
	return imscore.AKAResult{
		RES: []byte{0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33},
		CK:  []byte{0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11},
		IK:  []byte{0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22},
	}, nil
}

func TestCallStateMachine(t *testing.T) {
	agent := newTestAgent(t)
	call := NewCall(agent, callstate.DirectionOutbound, "call-1", "+8613800000000")

	if call.GetState() != callstate.StateIdle {
		t.Errorf("initial state = %s, want Idle", call.GetState())
	}
	// Invalid: skip to Connected.
	if err := call.Transition(callstate.StateConnected); err == nil {
		t.Error("Idle->Connected should be invalid")
	}
	// Valid path.
	for _, want := range []callstate.State{
		callstate.StateDialing,
		callstate.StateAlerting,
		callstate.StateConnecting,
		callstate.StateConnected,
		callstate.StateDisconnected,
		callstate.StateEnded,
	} {
		if err := call.Transition(want); err != nil {
			t.Fatalf("Transition(%s): %v", want, err)
		}
	}
	if !call.IsTerminalState() {
		t.Error("Ended should be terminal")
	}
	if call.IsConnected() {
		t.Error("ended call should not be connected")
	}
}

func TestCallDuration(t *testing.T) {
	agent := newTestAgent(t)
	call := NewCall(agent, callstate.DirectionOutbound, "call-1", "13800000000")
	call.SetStartTime(time.Now().Add(-5 * time.Second))
	if d := call.Duration(); d < 4*time.Second || d > 6*time.Second {
		t.Errorf("duration = %v, want ~5s", d)
	}
}

func TestAgentDialLifecycle(t *testing.T) {
	agent := newTestAgent(t)
	if err := agent.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer agent.Stop()

	var got []string
	agent.SetNotifier(func(ev events.Event) {
		got = append(got, ev.Type())
	})

	call, err := agent.Dial("+8613800000000")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if call.GetState() != callstate.StateDialing {
		t.Errorf("state = %s, want Dialing", call.GetState())
	}
	if !agent.IsBusy() {
		t.Error("agent should be busy after dial")
	}
	if err := agent.Hangup(call.CallID()); err != nil {
		t.Fatalf("Hangup: %v", err)
	}
	if call.GetState() != callstate.StateDisconnected {
		t.Errorf("state = %s, want Disconnected after hangup", call.GetState())
	}
	if agent.IsBusy() {
		t.Error("agent should not be busy after hangup")
	}
}

func TestAgentSimulateCall(t *testing.T) {
	agent := newTestAgent(t)
	call, err := agent.SimulateCall("+8613800000000")
	if err != nil {
		t.Fatalf("SimulateCall: %v", err)
	}
	if call.GetState() != callstate.StateConnected {
		t.Errorf("state = %s, want Connected", call.GetState())
	}
	snap := agent.Snapshot()
	if snap.ActiveCall == nil || snap.ActiveCall.State != "Connected" {
		t.Errorf("snapshot active = %+v", snap.ActiveCall)
	}
}

func TestAgentInboundAnswer(t *testing.T) {
	agent := newTestAgent(t)
	call := NewCall(agent, callstate.DirectionInbound, "call-in", "+8613800000000")
	if err := call.Transition(callstate.StateAlerting); err != nil {
		t.Fatalf("Transition(Alerting): %v", err)
	}
	agent.mu.Lock()
	agent.calls[call.CallID()] = call
	agent.activeCall = call
	agent.mu.Unlock()

	if err := agent.Answer(call.CallID()); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if call.GetState() != callstate.StateConnected {
		t.Errorf("state = %s, want Connected", call.GetState())
	}
}

func TestParseSDP(t *testing.T) {
	sdp := "v=0\r\n" +
		"o=- 123 456 IN IP4 10.0.0.1\r\n" +
		"s=VoWiFi call\r\n" +
		"c=IN IP4 10.0.0.1\r\n" +
		"t=0 0\r\n" +
		"m=audio 49170 RTP/AVP 96 97\r\n" +
		"a=rtpmap:96 AMR-WB/16000/1\r\n" +
		"a=rtpmap:97 telephone-event/8000\r\n" +
		"a=fmtp:96 mode-set=0,1,2\r\n"
	info, err := ParseSDP(sdp)
	if err != nil {
		t.Fatalf("ParseSDP: %v", err)
	}
	if len(info.Media) != 1 {
		t.Fatalf("media count = %d", len(info.Media))
	}
	if info.Media[0].Port != 49170 {
		t.Errorf("port = %d", info.Media[0].Port)
	}
	codec := info.FindCodec(96)
	if codec == nil {
		t.Fatal("codec 96 not found")
	}
	if codec.Encoding != "AMR-WB" || codec.ClockRate != 16000 {
		t.Errorf("codec = %+v", codec)
	}
	if codec.Fmtp != "mode-set=0,1,2" {
		t.Errorf("fmtp = %q", codec.Fmtp)
	}
	if addr := info.GetMediaAddress(); addr != "10.0.0.1" {
		t.Errorf("media addr = %q", addr)
	}
}

func TestRewriteSDP(t *testing.T) {
	sdp := "v=0\r\nc=IN IP4 10.0.0.1\r\nm=audio 49170 RTP/AVP 96\r\n"
	out := RewriteSDP(sdp, "192.168.1.5", 50000)
	if !strings.Contains(out, "c=IN IP4 192.168.1.5") {
		t.Errorf("rewritten SDP missing new IP: %q", out)
	}
	if !strings.Contains(out, "m=audio 50000 RTP/AVP 96") {
		t.Errorf("rewritten SDP missing new port: %q", out)
	}
}

func TestBuildIMSInvite(t *testing.T) {
	agent := newTestAgent(t)
	call := NewCall(agent, callstate.DirectionOutbound, "call-1", "+8613800000000")
	invite := BuildIMSInvite(agent, call)
	if !strings.HasPrefix(invite, "INVITE sip:8613800000000@") {
		t.Errorf("invite = %q", invite)
	}
	if !strings.Contains(invite, "Call-ID: call-1") {
		t.Errorf("invite missing Call-ID: %q", invite)
	}
	if !strings.Contains(invite, "Content-Type: application/sdp") {
		t.Errorf("invite missing SDP content type: %q", invite)
	}
	if !strings.Contains(invite, "m=audio") {
		t.Errorf("invite missing SDP body: %q", invite)
	}
}

func TestBuildIMSBye(t *testing.T) {
	agent := newTestAgent(t)
	call := NewCall(agent, callstate.DirectionOutbound, "call-1", "+8613800000000")
	bye := BuildIMSBye(agent, call)
	if !strings.HasPrefix(bye, "BYE sip:8613800000000@") {
		t.Errorf("bye = %q", bye)
	}
	if !strings.Contains(bye, "CSeq: 2 BYE") {
		t.Errorf("bye missing CSeq: %q", bye)
	}
}

func TestGatewayLifecycle(t *testing.T) {
	agent := newTestAgent(t)
	gw := NewGateway(agent)
	if err := gw.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer gw.Stop()
	if gw.GetAgent() != agent {
		t.Error("gateway agent mismatch")
	}
	status := gw.DeviceStatus()
	if status["registered"] != true {
		t.Errorf("status = %+v", status)
	}
	call, err := gw.SimulateCall("+8613800000000")
	if err != nil {
		t.Fatalf("SimulateCall: %v", err)
	}
	if call.GetState() != callstate.StateConnected {
		t.Errorf("state = %s", call.GetState())
	}
}

func TestExtractAndApplyPTMapping(t *testing.T) {
	offer, _ := ParseSDP("v=0\r\nm=audio 100 RTP/AVP 96\r\na=rtpmap:96 AMR-WB/16000/1\r\n")
	answer, _ := ParseSDP("v=0\r\nm=audio 200 RTP/AVP 8\r\na=rtpmap:8 AMR-WB/16000/1\r\n")
	m := ExtractAndApplyPTMapping(offer, answer)
	if m[8] != 96 {
		t.Errorf("mapping = %+v, want {8:96}", m)
	}
}
