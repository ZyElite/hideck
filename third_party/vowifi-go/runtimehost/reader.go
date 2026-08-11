package runtimehost

import "github.com/iniwex5/vowifi-go/internal/vowifi/runtimecore"

// NewReaderSIMAdapter returns a SIM adapter for reader-mode hosts, backed by
// the given SIM provider.
func NewReaderSIMAdapter(provider SIMProvider) SIMAdapter {
	return &simAdapter{inner: runtimecore.NewReaderSIMAdapter(provider)}
}
