package voicehost

import (
	"context"
	"testing"
	"time"
)

// fakeAgent drives a simulated call without the voice engine.
type fakeAgent struct {
	dialed string
	hungup string
}

func (f *fakeAgent) DialContext(_ context.Context, number string) (interface{}, error) {
	f.dialed = number
	return fakeCall{id: "call-1"}, nil
}
func (f *fakeAgent) HangupContext(_ context.Context, callID string) error {
	f.hungup = callID
	return nil
}
func (f *fakeAgent) Ready() bool  { return true }
func (f *fakeAgent) Start() error { return nil }
func (f *fakeAgent) Stop() error  { return nil }

type fakeCall struct{ id string }

func (c fakeCall) CallID() string { return c.id }

type fakeIncomingAgent struct {
	fakeAgent
	handler  func(IncomingCall)
	answered AnswerRequest
	rejected RejectRequest
}

func (f *fakeIncomingAgent) SetIncomingCallHandler(handler func(IncomingCall)) { f.handler = handler }
func (f *fakeIncomingAgent) IncomingCalls() []IncomingCall {
	return []IncomingCall{{DeviceID: "dev-1", CallID: "incoming-1", OfferSDP: "v=0\r\n"}}
}
func (f *fakeIncomingAgent) AnswerIncomingCall(_ context.Context, callID, sdp string) (AnswerResult, error) {
	f.answered = AnswerRequest{CallID: callID, SDP: sdp}
	return AnswerResult{CallID: callID, State: "Connected"}, nil
}
func (f *fakeIncomingAgent) RejectIncomingCall(callID string, statusCode int) error {
	f.rejected = RejectRequest{CallID: callID, StatusCode: statusCode}
	return nil
}

func TestGatewaySimulateCall(t *testing.T) {
	g := NewGateway()
	agent := &fakeAgent{}
	g.SetAgent("dev-1", agent)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := g.SimulateCall(ctx, "dev-1", SimulateCallRequest{Callee: "+8613800000000", HoldSeconds: 1})
	if err != nil {
		t.Fatalf("SimulateCall: %v", err)
	}
	if !res.Success {
		t.Errorf("result = %+v", res)
	}
	if agent.dialed != "+8613800000000" {
		t.Errorf("dialed = %q", agent.dialed)
	}
	if agent.hungup != "call-1" {
		t.Errorf("hung up = %q, want call-1", agent.hungup)
	}
	if g.GetAgent("dev-1") != agent {
		t.Error("GetAgent mismatch")
	}
}

func TestGatewayNoAgent(t *testing.T) {
	g := NewGateway()
	if _, err := g.SimulateCall(context.Background(), "dev-1", SimulateCallRequest{}); err == nil {
		t.Error("should error without agent")
	}
}

func TestGatewayExposesIncomingCallLifecycle(t *testing.T) {
	gateway := NewGateway()
	var delivered IncomingCall
	gateway.SetIncomingCallHandler(func(call IncomingCall) { delivered = call })
	agent := &fakeIncomingAgent{}
	if err := gateway.SetAgent("dev-1", agent); err != nil {
		t.Fatal(err)
	}
	agent.handler(IncomingCall{DeviceID: "dev-1", CallID: "incoming-1"})
	if delivered.CallID != "incoming-1" {
		t.Fatalf("delivered call = %+v", delivered)
	}
	calls, err := gateway.IncomingCalls("dev-1")
	if err != nil || len(calls) != 1 || calls[0].CallID != "incoming-1" {
		t.Fatalf("IncomingCalls calls=%+v err=%v", calls, err)
	}
	answer, err := gateway.AnswerIncomingCall(context.Background(), AnswerRequest{
		DeviceID: "dev-1", CallID: "incoming-1", SDP: "v=0\r\n",
	})
	if err != nil || answer.State != "Connected" || agent.answered.CallID != "incoming-1" {
		t.Fatalf("answer=%+v recorded=%+v err=%v", answer, agent.answered, err)
	}
	if err := gateway.RejectIncomingCall(RejectRequest{DeviceID: "dev-1", CallID: "incoming-2"}); err != nil {
		t.Fatal(err)
	}
	if agent.rejected.StatusCode != 486 {
		t.Fatalf("reject = %+v", agent.rejected)
	}
}
