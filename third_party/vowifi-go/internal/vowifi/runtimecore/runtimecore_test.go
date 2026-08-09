package runtimecore

import (
	"bytes"
	"reflect"
	"testing"

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
	if !reflect.DeepEqual(cfg.IKEProposals, carrierConfig.IKEProposals) {
		t.Fatalf("IKE proposals = %v, want %v", cfg.IKEProposals, carrierConfig.IKEProposals)
	}
	if !reflect.DeepEqual(cfg.ESPProposals, carrierConfig.ESPProposals) {
		t.Fatalf("ESP proposals = %v, want %v", cfg.ESPProposals, carrierConfig.ESPProposals)
	}
	if cfg.ReauthSeconds != 0 {
		t.Fatalf("reauth = %s", cfg.ReauthSeconds)
	}
}

func TestBuildSWUConfigPreservesOrderedCarrierProposals(t *testing.T) {
	carrierConfig := carrier.EffectiveCarrierConfig{
		AlgorithmPolicy: "balanced",
		IKEProposals: []string{
			"aes128-sha256-modp2048",
			"aes128-sha1-modp1024",
		},
		ESPProposals:         []string{"aes256gcm16", "aes128-sha1"},
		EnableLegacyCiphers:  true,
		AllowedLegacyCiphers: []string{"3des"},
	}
	cfg, err := BuildSWUConfig(
		&PreparedSessionStart{Carrier: carrierConfig},
		&recordingAKAProvider{},
	)
	if err != nil {
		t.Fatalf("BuildSWUConfig() error = %v", err)
	}
	if !reflect.DeepEqual(cfg.IKEProposals, carrierConfig.IKEProposals) ||
		!reflect.DeepEqual(cfg.ESPProposals, carrierConfig.ESPProposals) {
		t.Fatalf("proposal order not preserved: IKE=%v ESP=%v", cfg.IKEProposals, cfg.ESPProposals)
	}
	if cfg.AlgorithmPolicy != "balanced" || !cfg.EnableLegacyCiphers ||
		!reflect.DeepEqual(cfg.AllowedLegacyCiphers, []string{"3des"}) {
		t.Fatalf("algorithm policy not preserved: %+v", cfg)
	}
	carrierConfig.IKEProposals[0] = "changed"
	carrierConfig.AllowedLegacyCiphers[0] = "changed"
	if cfg.IKEProposals[0] == "changed" || cfg.AllowedLegacyCiphers[0] == "changed" {
		t.Fatal("BuildSWUConfig() retained carrier slice aliases")
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

func TestCleanupDataplaneInterfaceRequiresOwner(t *testing.T) {
	if err := CleanupDataplaneInterface(); err == nil {
		t.Fatal("CleanupDataplaneInterface reported success without an owning session")
	}
}
