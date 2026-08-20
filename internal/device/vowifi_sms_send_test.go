package device

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost"
	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
	"github.com/yibaiba/hideck/internal/config"
)

func TestSendVoWiFiSMSWhenReadySendsImmediately(t *testing.T) {
	runtime := &fakeVoWiFiSMSRuntime{state: runtimehost.State{SMSReady: true}}
	runtime.send = func(context.Context, string, string, messaging.SendOptions) (messaging.SendOutcome, error) {
		return messaging.SendOutcome{MessageID: "msg-1"}, nil
	}
	outcome, err := sendVoWiFiSMSWhenReady(context.Background(), voWiFiSMSSendRequest{
		DeviceID: "wwan0", To: "888", Text: "BAL", Updates: make(chan runtimehost.State),
		Runtime: func() voWiFiSMSRuntime { return runtime },
	})
	if err != nil || outcome.MessageID != "msg-1" || runtime.sendCount() != 1 {
		t.Fatalf("send outcome=%+v err=%v count=%d", outcome, err, runtime.sendCount())
	}
}

func TestSendVoWiFiSMSWhenReadyWaitsAcrossRecovery(t *testing.T) {
	failedRuntime := &fakeVoWiFiSMSRuntime{state: runtimehost.State{
		SMSReadyReason: "IMS registration is not ready",
	}}
	recoveredRuntime := &fakeVoWiFiSMSRuntime{state: runtimehost.State{
		SMSReady: true, SMSReadyReason: "IMS SMS receiver ready",
	}}
	recoveredRuntime.send = func(context.Context, string, string, messaging.SendOptions) (messaging.SendOutcome, error) {
		return messaging.SendOutcome{MessageID: "msg-recovered"}, nil
	}
	var runtimeMu sync.Mutex
	currentRuntime := voWiFiSMSRuntime(failedRuntime)
	updates := make(chan runtimehost.State, 1)
	checked := make(chan struct{}, 1)
	result := make(chan voWiFiSMSSendResult, 1)
	go func() {
		outcome, err := sendVoWiFiSMSWhenReady(context.Background(), voWiFiSMSSendRequest{
			DeviceID: "wwan1", To: "888", Text: "BAL", Updates: updates,
			Runtime: func() voWiFiSMSRuntime {
				runtimeMu.Lock()
				defer runtimeMu.Unlock()
				select {
				case checked <- struct{}{}:
				default:
				}
				return currentRuntime
			},
		})
		result <- voWiFiSMSSendResult{outcome: outcome, err: err}
	}()
	select {
	case <-checked:
	case <-time.After(time.Second):
		t.Fatal("send did not inspect the recovering runtime")
	}
	runtimeMu.Lock()
	currentRuntime = recoveredRuntime
	runtimeMu.Unlock()
	updates <- recoveredRuntime.State()
	select {
	case got := <-result:
		if got.err != nil || got.outcome.MessageID != "msg-recovered" ||
			failedRuntime.sendCount() != 0 || recoveredRuntime.sendCount() != 1 {
			t.Fatalf("send result=%+v err=%v old/new count=%d/%d", got.outcome, got.err,
				failedRuntime.sendCount(), recoveredRuntime.sendCount())
		}
	case <-time.After(time.Second):
		t.Fatal("send did not resume after SMS readiness recovered")
	}
}

func TestSendVoWiFiSMSWhenReadySurfacesRecoveryTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := sendVoWiFiSMSWhenReady(ctx, voWiFiSMSSendRequest{
		DeviceID: "wwan1", Updates: make(chan runtimehost.State),
		Runtime: func() voWiFiSMSRuntime { return nil },
	})
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "等待短信恢复失败") {
		t.Fatalf("timeout error = %v", err)
	}
}

func TestSendVoWiFiSMSWhenReadyDoesNotWaitForMissingSMSC(t *testing.T) {
	runtime := &fakeVoWiFiSMSRuntime{state: runtimehost.State{
		IMSReady: true, SMSReadyReason: "IMS SMSC is not configured",
	}}
	runtime.send = func(context.Context, string, string, messaging.SendOptions) (messaging.SendOutcome, error) {
		return messaging.SendOutcome{}, messaging.ErrSMSNotReady
	}
	_, err := sendVoWiFiSMSWhenReady(context.Background(), voWiFiSMSSendRequest{
		DeviceID: "wwan0", Updates: make(chan runtimehost.State),
		Runtime: func() voWiFiSMSRuntime { return runtime },
	})
	if !errors.Is(err, messaging.ErrSMSNotReady) || runtime.sendCount() != 1 {
		t.Fatalf("missing SMSC error=%v count=%d", err, runtime.sendCount())
	}
}

func TestShouldRouteSMSViaVoWiFiDuringConfiguredRecovery(t *testing.T) {
	tests := []struct {
		name     string
		config   config.DeviceConfig
		expected bool
	}{
		{name: "wifi recovery", config: config.DeviceConfig{VoWiFiEnabled: true, PhoneMode: "wifi"}, expected: true},
		{name: "cellular always recovery", config: config.DeviceConfig{VoWiFiEnabled: true, PhoneMode: "cellular", DataStrategy: "always"}, expected: true},
		{name: "cellular on demand idle", config: config.DeviceConfig{VoWiFiEnabled: true, PhoneMode: "cellular", DataStrategy: "on_demand"}, expected: false},
		{name: "disabled", config: config.DeviceConfig{VoWiFiEnabled: false}, expected: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewPool(&config.Config{})
			defer pool.cancel()
			pool.workers["wwan0"] = &Worker{ID: "wwan0", Config: tt.config}
			if got := pool.ShouldRouteSMSViaVoWiFi("wwan0"); got != tt.expected {
				t.Fatalf("ShouldRouteSMSViaVoWiFi() = %t, want %t", got, tt.expected)
			}
		})
	}
}

type voWiFiSMSSendResult struct {
	outcome messaging.SendOutcome
	err     error
}

type fakeVoWiFiSMSRuntime struct {
	mu    sync.Mutex
	state runtimehost.State
	sends int
	send  func(context.Context, string, string, messaging.SendOptions) (messaging.SendOutcome, error)
}

func (f *fakeVoWiFiSMSRuntime) State() runtimehost.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *fakeVoWiFiSMSRuntime) SendSMSWithOptions(
	ctx context.Context,
	to string,
	text string,
	options messaging.SendOptions,
) (messaging.SendOutcome, error) {
	f.mu.Lock()
	f.sends++
	send := f.send
	f.mu.Unlock()
	return send(ctx, to, text, options)
}

func (f *fakeVoWiFiSMSRuntime) sendCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sends
}
