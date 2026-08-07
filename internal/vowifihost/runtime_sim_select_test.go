package vowifihost

import (
	"errors"
	"testing"

	swusim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/runtimehost"
)

type simProviderStub struct{}

func (simProviderStub) CalculateAKA(rand16, autn16 []byte) (swusim.AKAResult, error) {
	return swusim.AKAResult{}, errors.New("test challenge")
}

func TestBuildVoWiFiSIMAdapterPrefersOverride(t *testing.T) {
	override := runtimehost.NewReaderSIMAdapter(simProviderStub{})
	got, err := buildVoWiFiSIMAdapter(override)
	if err != nil || got != override {
		t.Fatal("override 应被返回")
	}
	if _, err := buildVoWiFiSIMAdapter(nil); err == nil {
		t.Fatal("缺失 AKA provider 应在启动边界返回错误")
	}
}
