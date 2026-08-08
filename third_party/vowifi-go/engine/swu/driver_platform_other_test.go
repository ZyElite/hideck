//go:build !linux

package swu

import (
	"context"
	"testing"
)

func TestKernelDataplaneModesFailBeforeOpeningTransportOffLinux(t *testing.T) {
	for _, mode := range []string{DataplaneModeTUN, DataplaneModeXFRMI} {
		session := NewSession(&Config{DataplaneMode: mode, EPDGAddr: "127.0.0.1"})
		if err := session.Connect(context.Background()); err == nil {
			t.Fatalf("mode %q reached connection setup", mode)
		}
		if session.socket != nil {
			t.Fatalf("mode %q opened a transport", mode)
		}
	}
}
