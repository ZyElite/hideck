package runtimecore

import (
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/runtimehost/carrier"
)

const (
	aesCBCTransform       = 12
	prfHMACSHA512         = 7
	integrityHMACSHA512   = 14
	modp2048Group         = 14
	aes256KeyLengthBits   = 256
	maxCarrierReauthValue = int64(^uint64(0) >> 1)
)

type carrierAlgorithmSelection struct {
	ikeEncryption uint16
	ikeKeyBits    uint16
	ikePRF        uint16
	ikeIntegrity  uint16
	ikeDH         uint16
	espEncryption uint16
	espKeyBits    uint16
	espIntegrity  uint16
}

func applyCarrierAlgorithms(cfg *swu.Config, carrierConfig carrier.EffectiveCarrierConfig) error {
	if carrierConfig.ReauthIntervalSeconds < 0 {
		return fmt.Errorf("runtimecore: carrier reauth interval must not be negative")
	}
	selection, err := resolveCarrierAlgorithms(carrierConfig)
	if err != nil {
		return fmt.Errorf("runtimecore: carrier algorithms: %w", err)
	}
	if selection != nil {
		cfg.IKEEncryption = selection.ikeEncryption
		cfg.IKEEncryptionKeyBits = selection.ikeKeyBits
		cfg.IKEPRF = selection.ikePRF
		cfg.IKEIntegrity = selection.ikeIntegrity
		cfg.IKEDH = selection.ikeDH
		cfg.ESPEncryption = selection.espEncryption
		cfg.ESPEncryptionKeyBits = selection.espKeyBits
		cfg.ESPIntegrity = selection.espIntegrity
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

func resolveCarrierAlgorithms(cfg carrier.EffectiveCarrierConfig) (*carrierAlgorithmSelection, error) {
	if len(cfg.IKEProposals) == 0 && len(cfg.ESPProposals) == 0 {
		return nil, nil
	}
	if len(cfg.IKEProposals) != 1 || len(cfg.ESPProposals) != 1 {
		return nil, fmt.Errorf("exactly one IKE and ESP proposal is required")
	}
	if strings.TrimSpace(cfg.IKEProposals[0]) != carrier.IKEProposalAES256SHA512PRFSHA512MODP2048 {
		return nil, fmt.Errorf("unsupported IKE proposal %q", cfg.IKEProposals[0])
	}
	if strings.TrimSpace(cfg.ESPProposals[0]) != carrier.ESPProposalAES256SHA512 {
		return nil, fmt.Errorf("unsupported ESP proposal %q", cfg.ESPProposals[0])
	}
	return &carrierAlgorithmSelection{
		ikeEncryption: aesCBCTransform, ikeKeyBits: aes256KeyLengthBits,
		ikePRF: prfHMACSHA512, ikeIntegrity: integrityHMACSHA512, ikeDH: modp2048Group,
		espEncryption: aesCBCTransform, espKeyBits: aes256KeyLengthBits,
		espIntegrity: integrityHMACSHA512,
	}, nil
}
