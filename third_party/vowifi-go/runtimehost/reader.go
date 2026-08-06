package runtimehost

// NewReaderSIMAdapter returns a SIM adapter for reader-mode hosts, backed by
// the given SIM provider.
func NewReaderSIMAdapter(provider SIMProvider) SIMAdapter {
	return &simAdapter{provider: provider}
}
