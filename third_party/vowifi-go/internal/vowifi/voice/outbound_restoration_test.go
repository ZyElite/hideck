package voice

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

type recoveredOutboundAPI interface {
	HandleOutboundInvite(*sip.Request, sip.ServerTransaction)
	HandleCancel(*sip.Request, sip.ServerTransaction)
	HandleOutboundACK(*sip.Request)
	HandlePrack(*sip.Request, sip.ServerTransaction)
	SimulateCall(context.Context, SimulateCallRequest) (*SimulateCallResult, error)
}

var _ recoveredOutboundAPI = (*Agent)(nil)

type recoveredOutboundGatewayAPI interface {
	SimulateCall(context.Context, string, SimulateCallRequest) (*SimulateCallResult, error)
}

var _ recoveredOutboundGatewayAPI = (*Gateway)(nil)

func TestStructuredOutboundInviteUsesIMSAndClientTransactions(t *testing.T) {
	agent := newTestAgent(t)
	if err := agent.StartCurrent(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Stop() })
	request := mustClientRequest(t, sip.INVITE, "client-outbound-31", testClientSDP, "")
	transaction := newVoiceServerTransaction()

	agent.HandleOutboundInvite(request, transaction)
	response := waitVoiceResponse(t, transaction, 200)
	call := agent.ActiveCall()
	if call == nil || call.CallID() != "client-outbound-31" || call.CallState() != callstate.StateConnected {
		t.Fatalf("active call = %+v", call)
	}
	if response.CallID() == nil || response.CallID().Value() != "client-outbound-31" {
		t.Fatalf("client response Call-ID = %v", response.CallID())
	}
	if response.To() == nil || sipHeaderTag(response.To()) != "remote" {
		t.Fatalf("client response To = %v", response.To())
	}
	if !strings.Contains(string(response.Body()), "m=audio ") ||
		strings.Contains(string(response.Body()), "m=audio 33000 ") {
		t.Fatalf("client response SDP was not relayed: %q", response.Body())
	}
	if err := agent.Hangup(call.CallID()); err != nil {
		t.Fatal(err)
	}
}

func TestStructuredOutboundInviteRejectsBusyAndMissingOffer(t *testing.T) {
	agent := NewAgent("device-31", nil, nil)
	busy := NewCall(agent, callstate.DirectionOutbound, "busy-call", "43430")
	busy.SetStartTime(time.Now())
	if err := busy.TransitionChecked(callstate.StateDialing); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	agent.activeCall = busy
	agent.calls[busy.CallID()] = busy
	agent.mu.Unlock()
	t.Cleanup(busy.Cancel)

	busyTx := newVoiceServerTransaction()
	agent.HandleOutboundInvite(
		mustClientRequest(t, sip.INVITE, "busy-attempt", testClientSDP, ""), busyTx,
	)
	waitVoiceResponse(t, busyTx, 486)

	agent.mu.Lock()
	agent.activeCall = nil
	agent.mu.Unlock()
	missingOfferTx := newVoiceServerTransaction()
	agent.HandleOutboundInvite(
		mustClientRequest(t, sip.INVITE, "missing-offer", "", ""), missingOfferTx,
	)
	waitVoiceResponse(t, missingOfferTx, 488)
}

func TestStructuredCancelClosesLateAcceptedInvite(t *testing.T) {
	registrar := startLateAcceptedRegistrar(t)
	agent := newVoiceTestAgent(t, registrar.conn)
	if err := agent.StartCurrent(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Stop() })
	inviteTx := newVoiceServerTransaction()
	request := mustClientRequest(t, sip.INVITE, "cancel-outbound-31", testClientSDP, "")
	agent.HandleOutboundInvite(request, inviteTx)
	waitVoiceResponse(t, inviteTx, 180)
	call := waitForCancelableCall(t, agent)

	cancelTx := newVoiceServerTransaction()
	cancelRequest := mustClientRequest(t, sip.CANCEL, call.ClientCallID(), "", "")
	agent.HandleCancel(cancelRequest, cancelTx)
	waitVoiceResponse(t, cancelTx, 200)
	waitVoiceResponse(t, inviteTx, 487)

	select {
	case <-registrar.ack:
	case <-time.After(time.Second):
		t.Fatal("late accepted INVITE was not ACKed")
	}
	select {
	case <-registrar.bye:
	case <-time.After(time.Second):
		t.Fatal("late accepted INVITE was not closed with BYE")
	}
	if !call.HasLocalCancelSent() || agent.IsBusy() {
		t.Fatalf("cancel state: marked=%t busy=%t", call.HasLocalCancelSent(), agent.IsBusy())
	}
}

func TestStructuredCancelAndPRACKRejectUnknownDialog(t *testing.T) {
	agent := NewAgent("device-31", nil, nil)
	cancelTx := newVoiceServerTransaction()
	agent.HandleCancel(mustClientRequest(t, sip.CANCEL, "unknown", "", ""), cancelTx)
	waitVoiceResponse(t, cancelTx, 481)

	prackTx := newVoiceServerTransaction()
	agent.HandlePrack(mustClientRequest(t, sip.PRACK, "unknown", "", "RAck: 1 1 INVITE\r\n"), prackTx)
	waitVoiceResponse(t, prackTx, 481)
}

func TestRecoveredSimulateCallConnectsHoldsAndHangsUp(t *testing.T) {
	agent := newTestAgent(t)
	if err := agent.StartCurrent(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Stop() })
	connected := false
	result, err := agent.SimulateCall(context.Background(), SimulateCallRequest{
		Callee: "43430", HoldSeconds: 0, OnConnected: func() { connected = true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.Success || !connected || result.Reason != "定时正常挂断" {
		t.Fatalf("simulate result=%+v connected=%t", result, connected)
	}
	if agent.IsBusy() {
		t.Fatal("simulated call remained active after BYE")
	}
}

func mustClientRequest(
	t *testing.T,
	method sip.RequestMethod,
	callID string,
	body string,
	extra string,
) *sip.Request {
	t.Helper()
	contentType := ""
	if body != "" {
		contentType = "Content-Type: application/sdp\r\n"
	}
	raw := fmt.Sprintf(
		"%s sip:43430@client.example SIP/2.0\r\n"+
			"Via: SIP/2.0/UDP 127.0.0.1:5099;branch=z9hG4bK-client\r\n"+
			"From: <sip:1001@client.example>;tag=local\r\n"+
			"To: <sip:43430@client.example>\r\n"+
			"Call-ID: %s\r\nCSeq: 1 %s\r\n%s%sContent-Length: %d\r\n\r\n%s",
		method, callID, method, extra, contentType, len(body), body,
	)
	message, err := sip.ParseMessage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	request, ok := message.(*sip.Request)
	if !ok {
		t.Fatalf("parsed message type = %T", message)
	}
	return request
}

type voiceServerTransaction struct {
	mu        sync.Mutex
	responses chan *sip.Response
}

func newVoiceServerTransaction() *voiceServerTransaction {
	return &voiceServerTransaction{responses: make(chan *sip.Response, 8)}
}

func (tx *voiceServerTransaction) Terminate()                         {}
func (tx *voiceServerTransaction) OnTerminate(sip.FnTxTerminate) bool { return true }
func (tx *voiceServerTransaction) Done() <-chan struct{}              { return make(chan struct{}) }
func (tx *voiceServerTransaction) Err() error                         { return nil }
func (tx *voiceServerTransaction) Acks() <-chan *sip.Request          { return make(chan *sip.Request) }
func (tx *voiceServerTransaction) OnCancel(sip.FnTxCancel) bool       { return true }
func (tx *voiceServerTransaction) Respond(response *sip.Response) error {
	tx.mu.Lock()
	clone := response.Clone()
	tx.mu.Unlock()
	tx.responses <- clone
	return nil
}

func waitVoiceResponse(t *testing.T, tx *voiceServerTransaction, status int) *sip.Response {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case response := <-tx.responses:
			if response.StatusCode == status {
				return response
			}
		case <-deadline:
			t.Fatalf("timed out waiting for SIP %d", status)
		}
	}
}
