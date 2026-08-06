package common

import (
	"context"
	"net"
	"testing"
)

func TestNewTraceID(t *testing.T) {
	a, b := NewTraceID(), NewTraceID()
	if len(a) != 16 || a == b {
		t.Errorf("trace ids = %q %q", a, b)
	}
}

func TestWithTraceIDAndTraceID(t *testing.T) {
	ctx := WithTraceID(context.Background(), "abc123")
	if got := TraceID(ctx); got != "abc123" {
		t.Errorf("TraceID = %q", got)
	}
	if got := TraceID(nil); got != "" {
		t.Errorf("TraceID(nil) = %q", got)
	}
}

func TestPlmn3(t *testing.T) {
	if got := Plmn3("310", "26"); got != "310026" {
		t.Errorf("Plmn3(310,26) = %q", got)
	}
	if got := Plmn3("310", "260"); got != "310260" {
		t.Errorf("Plmn3(310,260) = %q", got)
	}
}

func TestIsIPv6AddrString(t *testing.T) {
	if !IsIPv6AddrString("2001:db8::1") {
		t.Error("IPv6 should be detected")
	}
	if IsIPv6AddrString("10.0.0.1") {
		t.Error("IPv4 should not be detected as IPv6")
	}
	if IsIPv6AddrString("not-an-ip") {
		t.Error("garbage should not be detected")
	}
}

func TestHostHasIP(t *testing.T) {
	// 127.0.0.1 is always present on loopback.
	if !HostHasIP(net.IPv4(127, 0, 0, 1)) {
		t.Error("loopback should be present")
	}
	if HostHasIP(net.IPv4(203, 0, 113, 1)) {
		t.Error("TEST-NET address should not be present")
	}
}

func TestRandomHex(t *testing.T) {
	a, b := RandomHex(8), RandomHex(8)
	if len(a) != 16 || a == b {
		t.Errorf("random hex = %q %q", a, b)
	}
}
