package phone

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"

	"github.com/pion/webrtc/v4"
)

func TestSetWebRTCPublicHostReportsResolutionFailure(t *testing.T) {
	settings := &webrtc.SettingEngine{}
	wantErr := errors.New("lookup failed")
	err := setWebRTCPublicHost(settings, "hideck.example.com", func(context.Context, string) ([]net.IPAddr, error) {
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("setWebRTCPublicHost() error = %v", err)
	}
}

func TestResolveWebRTCPublicIPs(t *testing.T) {
	lookupCalls := 0
	lookup := func(_ context.Context, host string) ([]net.IPAddr, error) {
		lookupCalls++
		if host != "hideck.example.com" {
			t.Fatalf("lookup host = %q", host)
		}
		return []net.IPAddr{
			{IP: net.ParseIP("203.0.113.10")},
			{IP: net.ParseIP("203.0.113.10")},
			{IP: net.ParseIP("2001:db8::10")},
		}, nil
	}

	got, err := resolveWebRTCPublicIPs("hideck.example.com", lookup)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"203.0.113.10", "2001:db8::10"}
	if !reflect.DeepEqual(got, want) || lookupCalls != 1 {
		t.Fatalf("resolveWebRTCPublicIPs() = %v, calls = %d", got, lookupCalls)
	}

	got, err = resolveWebRTCPublicIPs("203.0.113.20", lookup)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"203.0.113.20"}) || lookupCalls != 1 {
		t.Fatalf("IP literal result = %v, calls = %d", got, lookupCalls)
	}
}
