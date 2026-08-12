//go:build darwin || linux

package pcsc

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSystemBackendDiscoveryDoesNotPanic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := New().Readers(ctx)
	if err == nil || errors.Is(err, ErrUnavailable) || errors.Is(err, ErrReaderNotFound) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	t.Fatalf("system PC/SC discovery returned an unexpected error: %v", err)
}
