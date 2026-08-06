// Package common provides small shared helpers (trace IDs, PLMN formatting,
// address checks) used across the IMS stack.
//
// Reconstructed from the decompiled internal/vowifi/common.
package common

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
)

// traceIDKey is the context key for a trace ID.
type traceIDKey struct{}

// NewTraceID returns a random 16-hex-character trace ID.
func NewTraceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%016x", 0)
	}
	return hex.EncodeToString(b)
}

// WithTraceID returns a context carrying the given trace ID.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, traceID)
}

// TraceID returns the trace ID from the context, or "".
func TraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(traceIDKey{}).(string); ok {
		return v
	}
	return ""
}

// ToStrings converts a slice of fmt.Stringer values to strings.
func ToStrings[T fmt.Stringer](items []T) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.String())
	}
	return out
}

// Plmn3 formats a PLMN (MCC+MNC) as a 3-digit MNC-padded string: a 2-digit MNC
// is zero-padded to 3 digits.
func Plmn3(mcc, mnc string) string {
	if len(mnc) == 2 {
		mnc = "0" + mnc
	}
	return mcc + mnc
}

// IsIPv6AddrString reports whether s parses as an IPv6 address.
func IsIPv6AddrString(s string) bool {
	ip := net.ParseIP(strings.TrimSpace(s))
	return ip != nil && ip.To4() == nil
}

// HostHasIP reports whether the host has an interface with the given IP.
func HostHasIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		var n *net.IPNet
		if v, ok := a.(*net.IPNet); ok {
			n = v
		} else if v, ok := a.(*net.IPAddr); ok {
			n = &net.IPNet{IP: v.IP}
		}
		if n != nil && n.IP.Equal(ip) {
			return true
		}
	}
	return false
}

// RandomHex returns n random bytes as a hex string.
func RandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}
