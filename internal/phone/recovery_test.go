package phone

import (
	"context"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestDisconnectedMediaHangsUpAfterGraceAndCanRecover(t *testing.T) {
	gateway, store := newFakeVoiceGateway(), newMemoryCallStore()
	service := newPhoneTestService(t, gateway, store, 30*time.Millisecond)
	addActiveCallForRecovery(service, "call-recover", "media-recover")
	service.handleMediaState("media-recover", webrtc.PeerConnectionStateDisconnected)
	service.handleMediaState("media-recover", webrtc.PeerConnectionStateConnected)
	select {
	case callID := <-gateway.hangupCalls:
		t.Fatalf("recovered media unexpectedly hung up %s", callID)
	case <-time.After(50 * time.Millisecond):
	}

	service.handleMediaState("media-recover", webrtc.PeerConnectionStateDisconnected)
	select {
	case callID := <-gateway.hangupCalls:
		if callID != "call-recover" {
			t.Fatalf("hung up call = %q", callID)
		}
	case <-time.After(time.Second):
		t.Fatal("disconnected media was not hung up after grace")
	}
}

func TestServiceCloseHangsUpActiveCallAndUnsubscribes(t *testing.T) {
	gateway, store := newFakeVoiceGateway(), newMemoryCallStore()
	service, err := NewService(ServiceOptions{
		Gateway: gateway, Store: store, WebRTCUDPAddress: "127.0.0.1:0", RecoveryGrace: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	addActiveCallForRecovery(service, "call-close", "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if callID := <-gateway.hangupCalls; callID != "call-close" {
		t.Fatalf("closed call = %q", callID)
	}
	gateway.mu.Lock()
	unsubscribed := gateway.unsubscribed
	gateway.mu.Unlock()
	if unsubscribed != 2 {
		t.Fatalf("unsubscribe count = %d, want 2", unsubscribed)
	}
}

func addActiveCallForRecovery(service *Service, callID, mediaID string) {
	call := &activeCall{
		view:   CallView{CallID: callID, DeviceID: "dev-1", Status: StatusConnected, MediaID: mediaID},
		record: CallRecord{CallID: callID, DeviceID: "dev-1", Status: StatusConnected, StartedAt: time.Now()},
		owner:  "admin", lease: "lease-1", mediaID: mediaID,
		terminalDone: make(chan struct{}), finalizedDone: make(chan struct{}),
	}
	service.mu.Lock()
	service.calls[callID] = call
	service.deviceCalls[call.view.DeviceID] = callID
	if mediaID != "" {
		service.mediaCalls[mediaID] = callID
	}
	service.mu.Unlock()
}
