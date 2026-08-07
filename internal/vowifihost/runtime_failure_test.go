package vowifihost

import (
	"testing"

	"github.com/iniwex5/vowifi-go/runtimehost"
)

func TestTerminalRuntimeFailureRequiresExplicitError(t *testing.T) {
	if isTerminalRuntimeFailure(runtimehost.State{SessionState: "shutdown"}) {
		t.Fatal("normal shutdown must not trigger failure recovery")
	}
	if isTerminalRuntimeFailure(runtimehost.State{SessionState: "error"}) {
		t.Fatal("error state without an exposed cause must not trigger failure recovery")
	}
	if !isTerminalRuntimeFailure(runtimehost.State{SessionState: "error", LastError: "refresh timeout"}) {
		t.Fatal("explicit terminal runtime error was not recognized")
	}
}

func TestReleaseFailedRuntimeMakesInstanceRecoverable(t *testing.T) {
	manager := NewManager()
	instance := &runtimehost.Instance{}
	manager.RuntimeStore().SetInstance("wwan0", instance)
	state := runtimehost.State{
		SessionState: "error", LastErrorClass: "ims",
		LastReason: "IMS registration refresh failed", LastError: "refresh timeout",
	}

	if !manager.releaseFailedRuntime("wwan0", instance, state) {
		t.Fatal("failed runtime was not released")
	}
	if manager.Active("wwan0") || !manager.DesiredRecoverable("wwan0") {
		t.Fatal("released runtime is not eligible for desired-state recovery")
	}
	if got := instance.State().SessionState; got != "stopped" {
		t.Fatalf("instance state = %q, want stopped", got)
	}
	if manager.releaseFailedRuntime("wwan0", instance, state) {
		t.Fatal("same failed runtime was released twice")
	}
}
