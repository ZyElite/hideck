// Package runtimehostcarrier converts between the internal carrier
// configuration and the runtimehost carrier surface.
//
// Reconstructed from the decompiled internal/runtimehostcarrier.
package runtimehostcarrier

import (
	"github.com/iniwex5/vowifi-go/runtimehost/carrier"
)

// ToInternal converts a runtimehost carrier config to the internal form.
func ToInternal(cfg carrier.EffectiveCarrierConfig) carrier.EffectiveCarrierConfig {
	return cfg
}

// FromInternal converts an internal carrier config to the runtimehost form.
func FromInternal(cfg carrier.EffectiveCarrierConfig) carrier.EffectiveCarrierConfig {
	return cfg
}

// TemplateToInternal converts a runtimehost IMS register template to the
// internal form.
func TemplateToInternal(t carrier.IMSRegisterTemplate) carrier.IMSRegisterTemplate {
	return t
}

// TemplateFromInternal converts an internal IMS register template to the
// runtimehost form.
func TemplateFromInternal(t carrier.IMSRegisterTemplate) carrier.IMSRegisterTemplate {
	return t
}
