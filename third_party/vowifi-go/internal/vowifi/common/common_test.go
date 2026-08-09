package common

import (
	"context"
	"net"
	"regexp"
	"testing"
)

var (
	lowerHexPattern                                               = regexp.MustCompile(`^[0-9a-f]+$`)
	_               func() string                                 = NewTraceID
	_               func(context.Context, string) context.Context = WithTraceID
	_               func(context.Context) string                  = TraceID
	_               func([]net.IP) []string                       = ToStrings
	_               func(string) string                           = Plmn3
	_               func(string) bool                             = IsIPv6AddrString
	_               func(string) bool                             = HostHasIP
	_               func(int) string                              = RandomHex
)

func TestNewTraceID(t *testing.T) {
	first, second := NewTraceID(), NewTraceID()
	if len(first) != 16 || first == second || !isLowerHex(first) {
		t.Fatalf("trace IDs = %q, %q", first, second)
	}
}

func TestWithTraceIDAndTraceID(t *testing.T) {
	ctx := WithTraceID(context.Background(), "abc123")
	if got := TraceID(ctx); got != "abc123" {
		t.Fatalf("TraceID = %q", got)
	}
	generated := TraceID(WithTraceID(nil, ""))
	if len(generated) != 16 || !isLowerHex(generated) {
		t.Fatalf("generated trace ID = %q", generated)
	}
	if got := TraceID(nil); got != "" {
		t.Fatalf("TraceID(nil) = %q", got)
	}
	if got := TraceID(context.WithValue(context.Background(), traceIDKey{}, 1)); got != "" {
		t.Fatalf("TraceID(non-string) = %q", got)
	}
}

func TestToStringsSkipsNilAddresses(t *testing.T) {
	addresses := []net.IP{nil, net.IPv4(192, 0, 2, 1), net.ParseIP("2001:db8::1")}
	actual := ToStrings(addresses)
	want := []string{"192.0.2.1", "2001:db8::1"}
	if len(actual) != len(want) {
		t.Fatalf("ToStrings = %#v", actual)
	}
	for index := range want {
		if actual[index] != want[index] {
			t.Fatalf("ToStrings[%d] = %q, want %q", index, actual[index], want[index])
		}
	}
}

func TestPlmn3(t *testing.T) {
	tests := map[string]string{
		"": "", "  ": "", "1": "001", "26": "026", "260": "260",
		"1000": "1000", " abc ": "abc", "-1": "-01",
	}
	for input, want := range tests {
		if got := Plmn3(input); got != want {
			t.Errorf("Plmn3(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsIPv6AddrString(t *testing.T) {
	for _, value := range []string{"2001:db8::1", " [2001:db8::1] ", "scope::name"} {
		if !IsIPv6AddrString(value) {
			t.Errorf("IsIPv6AddrString(%q) = false", value)
		}
	}
	for _, value := range []string{"10.0.0.1", "name:5060", "not-an-ip"} {
		if IsIPv6AddrString(value) {
			t.Errorf("IsIPv6AddrString(%q) = true", value)
		}
	}
}

func TestHostHasIP(t *testing.T) {
	if !HostHasIP(" 127.0.0.1 ") {
		t.Fatal("loopback address was not found")
	}
	if HostHasIP("203.0.113.1") || HostHasIP("not-an-ip") {
		t.Fatal("non-host address was reported present")
	}
}

func TestRandomHexUsesCharacterLength(t *testing.T) {
	for _, length := range []int{-1, 0, 1, 8, 9, 16} {
		actual := RandomHex(length)
		wantLength := length
		if wantLength < 0 {
			wantLength = 0
		}
		if len(actual) != wantLength || !isLowerHex(actual) {
			t.Errorf("RandomHex(%d) = %q", length, actual)
		}
	}
	if first, second := RandomHex(16), RandomHex(16); first == second {
		t.Fatalf("random values collided: %q", first)
	}
}

func TestAdditiveCompatibilityHelpers(t *testing.T) {
	if got := JoinPLMN("310", "26"); got != "310026" {
		t.Fatalf("JoinPLMN = %q", got)
	}
	if got := RandomHexBytes(5); len(got) != 10 || !isLowerHex(got) {
		t.Fatalf("RandomHexBytes = %q", got)
	}
	if !HostHasParsedIP(net.IPv4(127, 0, 0, 1)) {
		t.Fatal("HostHasParsedIP(loopback) = false")
	}
}

func isLowerHex(value string) bool {
	return value == "" || lowerHexPattern.MatchString(value)
}
