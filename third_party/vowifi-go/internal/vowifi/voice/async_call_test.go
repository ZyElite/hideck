package voice

import (
	"context"
	"testing"
	"time"
)

func TestBeginDialReturnsRealCallIDBeforeFinalResponse(t *testing.T) {
	registrar := startControlledRejectingRegistrar(t, 486)
	agent := newVoiceTestAgent(t, registrar.conn)
	if err := agent.StartCurrent(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Stop() })

	returned := make(chan *Call, 1)
	errors := make(chan error, 1)
	go func() {
		call, err := agent.BeginDial(context.Background(), "43430", testClientSDP, "")
		if err != nil {
			errors <- err
			return
		}
		returned <- call
	}()

	select {
	case <-registrar.invite:
	case <-time.After(time.Second):
		t.Fatal("outbound INVITE was not observed")
	}
	select {
	case err := <-errors:
		t.Fatal(err)
	case call := <-returned:
		if call.CallID() == "" {
			t.Fatal("BeginDial returned an empty Call-ID")
		}
		active := agent.ActiveCall()
		if active == nil || active.CallID() != call.CallID() {
			t.Fatalf("active call = %+v, returned Call-ID = %q", active, call.CallID())
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("BeginDial waited for the final SIP response")
	}

	registrar.releaseResponse()
	deadline := time.Now().Add(time.Second)
	for agent.IsBusy() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if agent.IsBusy() {
		t.Fatal("rejected asynchronous call was not finalized")
	}
}
