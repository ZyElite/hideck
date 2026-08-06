package voicehost

import (
	"context"
	"testing"
	"time"
)

// fakeAgent drives a simulated call without the voice engine.
type fakeAgent struct {
	simulated string
	hungup    string
}

func (f *fakeAgent) SimulateCall(number string) (interface{}, error) {
	f.simulated = number
	return fakeCall{id: "call-1"}, nil
}
func (f *fakeAgent) Hangup(callID string) error { f.hungup = callID; return nil }
func (f *fakeAgent) Start() error               { return nil }
func (f *fakeAgent) Stop() error                { return nil }

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
	if agent.simulated != "+8613800000000" {
		t.Errorf("simulated = %q", agent.simulated)
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
