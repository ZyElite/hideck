package voice

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

type recoveredSessionTimerCallAPI interface {
	StartSessionTimer(func())
	StartSessionTimerCurrent(time.Duration) error
}

type inboundSessionRequest struct {
	localAddr string
	callID    string
	to        string
	method    string
	cseq      int
	extra     string
}

type inboundSessionFixture struct {
	registrar *inboundDialogRegistrar
	client    *net.UDPAddr
	call      *Call
	to        string
}

var _ recoveredSessionTimerCallAPI = (*Call)(nil)

func TestRecoveredSessionTimerInvokesCallbackOnce(t *testing.T) {
	call := NewCall(nil, callstate.DirectionOutbound, "session-timer-callback", "43430")
	t.Cleanup(call.Cancel)
	call.Timers.SessionExpires = 1
	fired := make(chan struct{}, 2)
	call.StartSessionTimer(func() { fired <- struct{}{} })

	select {
	case <-fired:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("session timer callback did not run")
	}
	select {
	case <-fired:
		t.Fatal("session timer callback ran more than once")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestSessionTimerReplacementAndStopAreRaceSafe(t *testing.T) {
	call := NewCall(nil, callstate.DirectionOutbound, "session-timer-race", "43430")
	t.Cleanup(call.Cancel)
	call.Timers.SessionExpires = 3600
	var callbacks atomic.Int64
	var workers sync.WaitGroup
	for range 64 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			call.StartSessionTimer(func() { callbacks.Add(1) })
			_ = call.stopSessionTimer()
		}()
	}
	workers.Wait()
	if err := call.stopSessionTimer(); err != nil {
		t.Fatal(err)
	}
	if callbacks.Load() != 0 || call.Timers.SessionTimer != nil {
		t.Fatalf("timer cleanup callbacks=%d timer=%v", callbacks.Load(), call.Timers.SessionTimer)
	}
}

func TestOutboundSessionTimerSendsRealUpdate(t *testing.T) {
	registrar := startReliableProvisionalRegistrarWithOptions(t, reliableRegistrarOptions{
		prackResponsesAfter: 1,
		finalSessionExpires: "1;refresher=uac",
	})
	agent := newVoiceTestAgent(t, registrar.conn)
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Stop() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	call, err := agent.dialContext(ctx, "+447942985429", testClientSDP)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case request := <-registrar.update:
		if got := voiceTestHeader(request, "Session-Expires"); got != "1" {
			t.Fatalf("UPDATE Session-Expires = %q", got)
		}
		if got := voiceTestHeader(request, "Content-Type"); got != "" {
			t.Fatalf("UPDATE Content-Type = %q", got)
		}
	case <-ctx.Done():
		t.Fatal("session timer did not send UPDATE through the IMS dialog")
	}
	if call.CallState() != callstate.StateConnected {
		t.Fatalf("call state after UPDATE = %s", call.CallState())
	}
	select {
	case request := <-registrar.update:
		t.Fatalf("one-shot v1.5.5 session timer sent a second UPDATE: %q", request)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestInboundNetworkUpdateReplacesSessionTimer(t *testing.T) {
	fixture := establishInboundSessionTimerCall(t)
	initialExpires, initialTimer := sessionTimerSnapshot(fixture.call)
	if initialExpires != 180 || initialTimer == nil {
		t.Fatalf("initial session timer expires=%d timer=%v", initialExpires, initialTimer)
	}

	sendInboundSessionUpdate(t, fixture, 240)
	expires, replaced := sessionTimerSnapshot(fixture.call)
	if expires != 240 || replaced == nil || replaced == initialTimer {
		t.Fatalf("updated session timer expires=%d timer=%v initial=%v", expires, replaced, initialTimer)
	}

	sendInboundSessionBYE(t, fixture)
	_, timer := sessionTimerSnapshot(fixture.call)
	if timer != nil {
		t.Fatalf("session timer remained after BYE: %v", timer)
	}
}

func establishInboundSessionTimerCall(t *testing.T) inboundSessionFixture {
	t.Helper()
	registrar := startInboundDialogRegistrar(t)
	agent := newVoiceTestAgent(t, registrar.conn)
	incoming := make(chan IncomingCall, 1)
	agent.SetIncomingCallHandler(func(call IncomingCall) { incoming <- call })
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Stop() })

	clientAddr := registrar.waitClient(t)
	imsPeer := listenVoiceUDP(t)
	clientPeer := listenVoiceUDP(t)
	callID := "session-timer-inbound-update"
	invite := inboundNetworkInvite(registrar.conn.LocalAddr().String(), callID,
		voiceSDP(imsPeer.LocalAddr().(*net.UDPAddr).Port))
	invite = strings.Replace(invite, "Content-Type: application/sdp\r\n",
		"Session-Expires: 180;refresher=uas\r\nContent-Type: application/sdp\r\n", 1)
	sendInboundWireRequest(t, registrar, clientAddr, invite)
	_ = registrar.waitStatus(t, 180)

	var delivered IncomingCall
	select {
	case delivered = <-incoming:
	case <-time.After(2 * time.Second):
		t.Fatal("network INVITE did not reach the Agent")
	}
	if _, err := agent.AnswerWithSDP(delivered.CallID,
		voiceSDP(clientPeer.LocalAddr().(*net.UDPAddr).Port)); err != nil {
		t.Fatal(err)
	}
	accepted := registrar.waitStatus(t, 200)
	to := voiceTestHeader(accepted, "To")
	sendInboundWireRequest(t, registrar, clientAddr,
		inboundNetworkACK(registrar.conn.LocalAddr().String(), callID, to))
	call := agent.callByID(callID)
	return inboundSessionFixture{
		registrar: registrar, client: clientAddr, call: call, to: to,
	}
}

func sendInboundSessionUpdate(t *testing.T, fixture inboundSessionFixture, expires int) {
	t.Helper()
	sendInboundWireRequest(t, fixture.registrar, fixture.client,
		inboundNetworkSessionRequest(inboundSessionRequest{
			localAddr: fixture.registrar.conn.LocalAddr().String(), callID: fixture.call.CallID(),
			to: fixture.to, method: "UPDATE", cseq: 2,
			extra: fmt.Sprintf("Session-Expires: %d;refresher=uas\r\n", expires),
		}))
	_ = fixture.registrar.waitResponse(t, func(raw string) bool {
		return strings.HasPrefix(raw, "SIP/2.0 200 ") &&
			strings.HasSuffix(voiceTestHeader(raw, "CSeq"), " UPDATE")
	}, "UPDATE 200")
}

func sendInboundSessionBYE(t *testing.T, fixture inboundSessionFixture) {
	t.Helper()
	sendInboundWireRequest(t, fixture.registrar, fixture.client,
		inboundNetworkSessionRequest(inboundSessionRequest{
			localAddr: fixture.registrar.conn.LocalAddr().String(), callID: fixture.call.CallID(),
			to: fixture.to, method: "BYE", cseq: 3,
		}))
	_ = fixture.registrar.waitResponse(t, func(raw string) bool {
		return strings.HasPrefix(raw, "SIP/2.0 200 ") &&
			strings.HasSuffix(voiceTestHeader(raw, "CSeq"), " BYE")
	}, "BYE 200")
	select {
	case <-fixture.call.Done:
	case <-time.After(time.Second):
		t.Fatal("BYE cleanup did not complete")
	}
}

func sessionTimerSnapshot(call *Call) (int, *time.Timer) {
	if call == nil {
		return 0, nil
	}
	call.SessionTimerMu.Lock()
	defer call.SessionTimerMu.Unlock()
	return call.SessionExpires, call.SessionTimer
}

func inboundNetworkSessionRequest(cfg inboundSessionRequest) string {
	return fmt.Sprintf(
		"%s sip:user@ims.example.com SIP/2.0\r\nVia: SIP/2.0/UDP %s;rport;branch=z9hG4bK-%s-%d\r\n"+
			"From: <sip:+447700900123@ims.example.com>;tag=remote-28\r\nTo: %s\r\n"+
			"Call-ID: %s\r\nCSeq: %d %s\r\n%sContent-Length: 0\r\n\r\n",
		cfg.method, cfg.localAddr, strings.ToLower(cfg.method), cfg.cseq, cfg.to,
		cfg.callID, cfg.cseq, cfg.method, cfg.extra,
	)
}
