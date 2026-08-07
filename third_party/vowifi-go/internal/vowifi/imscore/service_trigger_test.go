package imscore

import (
	"net"
	"strings"
	"testing"
)

func TestTriggerRegisterImmediateReturnsRegistrationFailure(t *testing.T) {
	service, err := New(&IMSConfig{
		Domain: "ims.example", LocalIP: net.IPv4(10, 0, 0, 2),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	service.Stop()
	err = service.TriggerRegisterImmediate()
	if err == nil || !strings.Contains(err.Error(), "service stopped") {
		t.Fatalf("TriggerRegisterImmediate error = %v", err)
	}
	if service.IsRegistered() {
		t.Fatalf("registration state = %s", service.RegState())
	}
}
