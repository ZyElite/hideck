package runtimecore

import (
	"fmt"
	"time"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/runtimehost/carrier"
)

const maxCarrierReauthValue = int64(^uint64(0) >> 1)

func applyCarrierAlgorithms(cfg *swu.Config, carrierConfig carrier.EffectiveCarrierConfig) error {
	if carrierConfig.ReauthIntervalSeconds < 0 {
		return fmt.Errorf("runtimecore: carrier reauth interval must not be negative")
	}
	cfg.AlgorithmPolicy = carrierConfig.AlgorithmPolicy
	cfg.IKEProposals = append([]string(nil), carrierConfig.IKEProposals...)
	cfg.ESPProposals = append([]string(nil), carrierConfig.ESPProposals...)
	cfg.EnableLegacyCiphers = carrierConfig.EnableLegacyCiphers
	cfg.AllowedLegacyCiphers = append([]string(nil), carrierConfig.AllowedLegacyCiphers...)
	if err := swu.ValidateProposalConfig(cfg); err != nil {
		return fmt.Errorf("runtimecore: carrier algorithms: %w", err)
	}
	if carrierConfig.ReauthIntervalSeconds > 0 {
		seconds := int64(carrierConfig.ReauthIntervalSeconds)
		if seconds > maxCarrierReauthValue/int64(time.Second) {
			return fmt.Errorf("reauth interval %d seconds overflows duration", seconds)
		}
		cfg.ReauthSeconds = time.Duration(seconds) * time.Second
	}
	return nil
}
