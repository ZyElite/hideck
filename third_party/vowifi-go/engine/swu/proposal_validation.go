package swu

import "fmt"

// ValidateProposalConfig parses and capability-filters the configured IKE and
// ESP proposal lists without creating a session.
func ValidateProposalConfig(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("swu: nil proposal config")
	}
	if _, _, _, err := buildIKEProposals(cfg, nil, 0); err != nil {
		return fmt.Errorf("IKE proposals: %w", err)
	}
	if _, err := buildESPProposals(cfg, nil); err != nil {
		return fmt.Errorf("ESP proposals: %w", err)
	}
	return nil
}
