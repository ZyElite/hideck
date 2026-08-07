package runtimecore

import (
	"bytes"
	"testing"
	"time"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
	"github.com/iniwex5/vowifi-go/runtimehost/carrier"
)

type recordingAKAProvider struct {
	calls int
}

func (p *recordingAKAProvider) CalculateAKA(rand16, autn16 []byte) (enginesim.AKAResult, error) {
	p.calls++
	return enginesim.AKAResult{RES: append([]byte(nil), rand16...), CK: append([]byte(nil), autn16...)}, nil
}

func TestBuildSWUConfigInjectsAKAProvider(t *testing.T) {
	provider := &recordingAKAProvider{}
	prepared := &PreparedSessionStart{
		Profile:  profile.Profile{IMSI: "234102356143376", MCC: "234", MNC: "10"},
		EPDGAddr: "epdg.example.com",
		APN:      "ims",
	}
	cfg, err := BuildSWUConfig(prepared, provider)
	if err != nil {
		t.Fatalf("BuildSWUConfig() error = %v", err)
	}
	rand16, autn16 := bytes.Repeat([]byte{0x11}, 16), bytes.Repeat([]byte{0x22}, 16)
	result, err := cfg.AKAProvider.CalculateAKA(rand16, autn16)
	if err != nil || provider.calls != 1 || !bytes.Equal(result.RES, rand16) {
		t.Fatalf("AKA delegation result=%+v calls=%d err=%v", result, provider.calls, err)
	}
	if cfg.APN != "ims" {
		t.Fatalf("SWu APN = %q, want ims", cfg.APN)
	}
}

func TestBuildSWUConfigRejectsMissingAKAProvider(t *testing.T) {
	if _, err := BuildSWUConfig(&PreparedSessionStart{}, nil); err == nil {
		t.Fatal("BuildSWUConfig() error=nil, want missing AKA provider")
	}
	if _, err := BuildSWUConfig(nil, &recordingAKAProvider{}); err == nil {
		t.Fatal("BuildSWUConfig() error=nil, want nil prepared session")
	}
}

func TestBuildSWUConfigAppliesGiffgaffAlgorithms(t *testing.T) {
	provider := &recordingAKAProvider{}
	carrierConfig := carrier.ResolveEffectiveCarrierConfig(carrier.EffectiveCarrierConfigInput{
		MCC: "234", MNC: "10",
	})
	cfg, err := BuildSWUConfig(&PreparedSessionStart{Carrier: carrierConfig}, provider)
	if err != nil {
		t.Fatalf("BuildSWUConfig() error = %v", err)
	}
	if cfg.IKEEncryption != 12 || cfg.IKEEncryptionKeyBits != 256 || cfg.IKEPRF != 7 ||
		cfg.IKEIntegrity != 14 || cfg.IKEDH != 14 {
		t.Fatalf("IKE config = %+v", cfg)
	}
	if cfg.ESPEncryption != 12 || cfg.ESPEncryptionKeyBits != 256 || cfg.ESPIntegrity != 14 {
		t.Fatalf("ESP config = %+v", cfg)
	}
	if cfg.ReauthSeconds != 180*time.Second {
		t.Fatalf("reauth = %s", cfg.ReauthSeconds)
	}
}

func TestBuildSWUConfigRejectsUnknownCarrierProposal(t *testing.T) {
	prepared := &PreparedSessionStart{Carrier: carrier.EffectiveCarrierConfig{
		IKEProposals: []string{"unknown"}, ESPProposals: []string{"aes256-sha512"},
	}}
	if _, err := BuildSWUConfig(prepared, &recordingAKAProvider{}); err == nil {
		t.Fatal("BuildSWUConfig() error=nil, want unsupported proposal")
	}
}
