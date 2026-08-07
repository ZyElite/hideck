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
