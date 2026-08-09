package runtimecore

import (
	"fmt"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
	"github.com/iniwex5/vowifi-go/internal/vowifi/simauth"
)

type readerSIMAdapter struct {
	provider enginesim.AKAProvider
}

func NewReaderSIMAdapter(provider enginesim.AKAProvider) *readerSIMAdapter {
	return &readerSIMAdapter{provider: provider}
}

func (adapter *readerSIMAdapter) EPDGSIMProvider(_ profile.AuthPlan) enginesim.AKAProvider {
	if adapter != nil && adapter.provider != nil {
		return adapter.provider
	}
	return unsupportedSWUAKAProvider{message: "SIM reader has no SWu AKA provider"}
}

func (adapter *readerSIMAdapter) IMSAKAProvider(plan profile.AuthPlan) simauth.AKAProvider {
	plan = plan.Normalize()
	if adapter == nil || adapter.provider == nil {
		return unsupportedIMSAKAProvider{message: "SIM reader has no IMS AKA provider"}
	}
	if plan.IMSApp != profile.AKAAppISIM && plan.IMSApp != profile.AKAAppISIMStrict {
		return readerAKAProviderAdapter{provider: adapter.provider}
	}
	provider, ok := adapter.provider.(enginesim.ISIMAKAProvider)
	if !ok {
		return unsupportedIMSAKAProvider{message: "SIM reader does not support ISIM AKA"}
	}
	return readerISIMAKAProviderAdapter{provider: provider}
}

func (adapter *readerSIMAdapter) IMSIdentityProvider() profile.Provider {
	if adapter == nil || adapter.provider == nil {
		return nil
	}
	provider, _ := adapter.provider.(profile.Provider)
	return provider
}

type readerAKAProviderAdapter struct {
	provider enginesim.AKAProvider
}

func (adapter readerAKAProviderAdapter) CalculateAKA(rand16, autn16 []byte) (simauth.AKAResult, error) {
	if adapter.provider == nil {
		return simauth.AKAResult{}, fmt.Errorf("runtimecore: IMS AKA provider is unavailable")
	}
	result, err := adapter.provider.CalculateAKA(rand16, autn16)
	return cloneAKAResult(result), err
}

type readerISIMAKAProviderAdapter struct {
	provider enginesim.ISIMAKAProvider
}

func (adapter readerISIMAKAProviderAdapter) CalculateAKA(rand16, autn16 []byte) (simauth.AKAResult, error) {
	if adapter.provider == nil {
		return simauth.AKAResult{}, fmt.Errorf("runtimecore: ISIM AKA provider is unavailable")
	}
	result, err := adapter.provider.CalculateISIMAKA(rand16, autn16)
	return cloneAKAResult(result), err
}

type unsupportedSWUAKAProvider struct{ message string }

func (provider unsupportedSWUAKAProvider) CalculateAKA(_, _ []byte) (enginesim.AKAResult, error) {
	return enginesim.AKAResult{}, fmt.Errorf("%s", provider.message)
}

type unsupportedIMSAKAProvider struct{ message string }

func (provider unsupportedIMSAKAProvider) CalculateAKA(_, _ []byte) (simauth.AKAResult, error) {
	return simauth.AKAResult{}, fmt.Errorf("%s", provider.message)
}

func cloneAKAResult(result enginesim.AKAResult) enginesim.AKAResult {
	return enginesim.AKAResult{
		RES: append([]byte(nil), result.RES...), CK: append([]byte(nil), result.CK...),
		IK: append([]byte(nil), result.IK...), AUTS: append([]byte(nil), result.AUTS...),
	}
}
