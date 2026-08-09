package common

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type traceIDKey struct{}

// NewTraceID returns a random 16-character hexadecimal trace ID.
func NewTraceID() string {
	var raw [8]byte
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}

// WithTraceID returns a context carrying traceID. A nil context is replaced
// with Background, and an empty ID is populated with a fresh value.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if traceID == "" {
		traceID = NewTraceID()
	}
	return context.WithValue(ctx, traceIDKey{}, traceID)
}

// TraceID returns the trace ID carried by ctx, or an empty string.
func TraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	traceID, _ := ctx.Value(traceIDKey{}).(string)
	return traceID
}
