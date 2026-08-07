package runtimehost

import (
	"testing"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
)

type readerAKAProviderStub struct{}

func (*readerAKAProviderStub) CalculateAKA(rand16, autn16 []byte) (enginesim.AKAResult, error) {
	return enginesim.AKAResult{}, nil
}

func TestNewReaderSIMAdapterExposesInjectedAKAProvider(t *testing.T) {
	provider := &readerAKAProviderStub{}
	adapter := NewReaderSIMAdapter(provider)
	if adapter == nil || adapter.AKAProvider() != provider {
		t.Fatalf("AKAProvider() = %T, want injected provider", adapter.AKAProvider())
	}
}
