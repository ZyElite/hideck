package swu

import "testing"

func TestValidateProposalConfig(t *testing.T) {
	cfg := &Config{
		IKEProposals: []string{
			"aes128-sha256-modp2048",
			"aes256-sha1-modp1024",
		},
		ESPProposals: []string{
			"aes256gcm16",
			"aes128-sha256",
		},
	}
	if err := ValidateProposalConfig(cfg); err != nil {
		t.Fatalf("ValidateProposalConfig() error = %v", err)
	}
	if err := ValidateProposalConfig(nil); err == nil {
		t.Fatal("ValidateProposalConfig(nil) error = nil")
	}
	cfg.IKEProposals[0] = "unknown"
	if err := ValidateProposalConfig(cfg); err == nil {
		t.Fatal("ValidateProposalConfig() accepted an unknown IKE proposal")
	}
}
