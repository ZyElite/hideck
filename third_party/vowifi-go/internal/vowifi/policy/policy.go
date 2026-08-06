// Package policy resolves the effective carrier (PLMN) configuration for a
// VoWiFi session: e911 availability, IMS registration template, VoWiFi policy
// (blocked MCCs) and runtime carrier overrides.
//
// Reconstructed from the decompiled internal/vowifi/policy.
package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// IMSRegisterTemplate is the IMS registration template for a carrier.
type IMSRegisterTemplate struct {
	// Domain is the IMS domain (e.g. "ims.mnc026.mcc310.3gppnetwork.org").
	Domain string
	// EPDGAddr is the ePDG address.
	EPDGAddr string
	// RegisterPolicy is the registration policy ("auto" | "manual").
	RegisterPolicy string
	// SecAgreeMode enables the security-agreement (sec-agree) flow.
	SecAgreeMode bool
	// Transport is the SIP transport ("udp" | "tcp").
	Transport string
	// SMSReceiverTransport is the SMS receiver transport.
	SMSReceiverTransport string
	// SMSRoutingMethod is the SMS routing method.
	SMSRoutingMethod string
	// IdentitySource is the IMS identity source.
	IdentitySource string
	// DNSServer overrides the DNS server for registrar discovery.
	DNSServer string
}

// E911Config describes the carrier's e911 (emergency address) service.
type E911Config struct {
	Enabled  bool
	Provider string
}

// CarrierPlan is the carrier plan for a PLMN.
type CarrierPlan struct {
	MCC      string
	MNC      string
	PresetID string
	E911     E911Config
	IMS      IMSRegisterTemplate
}

// IsZero reports whether the plan is empty.
func (p *CarrierPlan) IsZero() bool {
	return p == nil || (p.MCC == "" && p.MNC == "" && p.PresetID == "" && p.IMS.Domain == "")
}

// EffectiveCarrierConfig is the resolved carrier configuration.
type EffectiveCarrierConfig struct {
	MCC      string
	MNC      string
	PresetID string
	E911     E911Config
	IMS      IMSRegisterTemplate
}

// CarrierOverride overrides a carrier's configuration at runtime.
type CarrierOverride struct {
	MCC      string
	MNC      string
	PresetID string
	E911     E911Config
	IMS      IMSRegisterTemplate
}

// VoWiFiPolicyBlockError is returned when the carrier's MCC is blocked.
type VoWiFiPolicyBlockError struct {
	MCC string
}

func (e *VoWiFiPolicyBlockError) Error() string {
	return "vowifi blocked for MCC " + e.MCC
}

func (e *VoWiFiPolicyBlockError) Unwrap() error {
	return nil
}

// NewVoWiFiBlockedMCCError returns an error indicating VoWiFi is blocked.
func NewVoWiFiBlockedMCCError(mcc string) error {
	return &VoWiFiPolicyBlockError{MCC: mcc}
}

// IsVoWiFiBlockedMCC reports whether VoWiFi is blocked for the given MCC.
func IsVoWiFiBlockedMCC(mcc string) bool {
	return blockedMCCs[strings.TrimSpace(mcc)]
}

// blockedMCCs is the set of MCCs where VoWiFi is not offered.
var blockedMCCs = map[string]bool{
	"460": true, // China Mobile
}

// defaultPresets are the built-in carrier presets.
var defaultPresets = []EffectiveCarrierConfig{
	{
		MCC: "310", MNC: "280", PresetID: "att",
		E911: E911Config{Enabled: true, Provider: "att"},
		IMS: IMSRegisterTemplate{
			Domain:               "ims.mnc280.mcc310.3gppnetwork.org",
			EPDGAddr:             "epdg.epc.att.net",
			RegisterPolicy:       "auto",
			SecAgreeMode:         true,
			Transport:            "udp",
			SMSReceiverTransport: "sip",
			SMSRoutingMethod:     "sip",
			IdentitySource:       "isim",
		},
	},
}

// DefaultCarrierIMSDomain returns the default IMS domain for a PLMN.
func DefaultCarrierIMSDomain(mcc, mnc string) string {
	if len(mnc) == 2 {
		mnc = "0" + mnc
	}
	return fmt.Sprintf("ims.mnc%s.mcc%s.3gppnetwork.org", mnc, mcc)
}

// DefaultCarrierStandardEPDGAddr returns the default ePDG address.
func DefaultCarrierStandardEPDGAddr() string {
	return "epdg.epc.att.net"
}

// DefaultIMSRegisterTemplate returns the default IMS registration template.
func DefaultIMSRegisterTemplate() IMSRegisterTemplate {
	return IMSRegisterTemplate{
		RegisterPolicy:       "auto",
		SecAgreeMode:         true,
		Transport:            "udp",
		SMSReceiverTransport: "sip",
		SMSRoutingMethod:     "sip",
		IdentitySource:       "isim",
	}
}

// IMSRegisterTemplateSecAgreeMode reports whether the template uses sec-agree.
func IMSRegisterTemplateSecAgreeMode(t IMSRegisterTemplate) bool {
	return t.SecAgreeMode
}

// NormalizeIMSRegisterPolicy normalizes the register policy string.
func NormalizeIMSRegisterPolicy(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "auto", "manual":
		return strings.ToLower(strings.TrimSpace(p))
	default:
		return "auto"
	}
}

// NormalizeIMSRegisterTemplate normalizes a template's fields.
func NormalizeIMSRegisterTemplate(t IMSRegisterTemplate) IMSRegisterTemplate {
	t.RegisterPolicy = NormalizeIMSRegisterPolicy(t.RegisterPolicy)
	t.Domain = strings.TrimSpace(t.Domain)
	t.EPDGAddr = strings.TrimSpace(t.EPDGAddr)
	t.Transport = NormalizeIMSTransport(t.Transport)
	t.SMSReceiverTransport = NormalizeSMSReceiverTransport(t.SMSReceiverTransport)
	t.SMSRoutingMethod = NormalizeSMSRoutingMethod(t.SMSRoutingMethod)
	t.IdentitySource = NormalizeIMSIdentitySource(t.IdentitySource)
	t.DNSServer = NormalizeCarrierDNSServer(t.DNSServer)
	return t
}

// NormalizeIMSDomain normalizes an IMS domain.
func NormalizeIMSDomain(d string) string {
	return strings.TrimSpace(strings.ToLower(d))
}

// NormalizeIMSTransport normalizes a SIP transport.
func NormalizeIMSTransport(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "tcp":
		return "tcp"
	case "tls":
		return "tls"
	default:
		return "udp"
	}
}

// NormalizeSMSReceiverTransport normalizes the SMS receiver transport.
func NormalizeSMSReceiverTransport(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "sip":
		return "sip"
	default:
		return "sip"
	}
}

// NormalizeSMSRoutingMethod normalizes the SMS routing method.
func NormalizeSMSRoutingMethod(m string) string {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "sip", "smsc":
		return strings.ToLower(strings.TrimSpace(m))
	default:
		return "sip"
	}
}

// NormalizeIMSIdentitySource normalizes the identity source.
func NormalizeIMSIdentitySource(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "isim", "usim", "sim":
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return "isim"
	}
}

// NormalizeCarrierDNSServer normalizes a DNS server address.
func NormalizeCarrierDNSServer(s string) string {
	return strings.TrimSpace(s)
}

// NormalizeE911Policy normalizes the e911 policy.
func NormalizeE911Policy(e E911Config) E911Config {
	e.Provider = strings.TrimSpace(e.Provider)
	if e.Provider == "" {
		e.Enabled = false
	}
	return e
}

// NormalizeCarrierOverride normalizes a carrier override.
func NormalizeCarrierOverride(o CarrierOverride) CarrierOverride {
	o.MCC = strings.TrimSpace(o.MCC)
	o.MNC = strings.TrimSpace(o.MNC)
	o.PresetID = strings.TrimSpace(o.PresetID)
	o.E911 = NormalizeE911Policy(o.E911)
	o.IMS = NormalizeIMSRegisterTemplate(o.IMS)
	return o
}

// GetGlobalDefaultConfig returns the global default carrier config.
func GetGlobalDefaultConfig() EffectiveCarrierConfig {
	return EffectiveCarrierConfig{
		IMS: DefaultIMSRegisterTemplate(),
	}
}

// MergeFromPreset merges a preset into the config.
func (c *EffectiveCarrierConfig) MergeFromPreset(preset EffectiveCarrierConfig) {
	if c == nil {
		return
	}
	if c.PresetID == "" {
		c.PresetID = preset.PresetID
	}
	if !c.E911.Enabled {
		c.E911 = preset.E911
	}
	if c.IMS.Domain == "" {
		c.IMS.Domain = preset.IMS.Domain
	}
	if c.IMS.EPDGAddr == "" {
		c.IMS.EPDGAddr = preset.IMS.EPDGAddr
	}
	if c.IMS.RegisterPolicy == "" {
		c.IMS.RegisterPolicy = preset.IMS.RegisterPolicy
	}
	if !c.IMS.SecAgreeMode {
		c.IMS.SecAgreeMode = preset.IMS.SecAgreeMode
	}
	if c.IMS.Transport == "" {
		c.IMS.Transport = preset.IMS.Transport
	}
}

// CarrierPlanFromEffectiveConfig converts an effective config to a plan.
func CarrierPlanFromEffectiveConfig(c EffectiveCarrierConfig) CarrierPlan {
	return CarrierPlan{
		MCC:      c.MCC,
		MNC:      c.MNC,
		PresetID: c.PresetID,
		E911:     c.E911,
		IMS:      c.IMS,
	}
}

// EffectiveCarrierConfigFromCarrierPlan converts a plan to an effective config.
func EffectiveCarrierConfigFromCarrierPlan(p CarrierPlan) EffectiveCarrierConfig {
	return EffectiveCarrierConfig{
		MCC:      p.MCC,
		MNC:      p.MNC,
		PresetID: p.PresetID,
		E911:     p.E911,
		IMS:      p.IMS,
	}
}

// plmnKey builds the PLMN lookup key.
func plmnKey(mcc, mnc string) string {
	return strings.TrimSpace(mcc) + "-" + strings.TrimSpace(mnc)
}

// ResolveEffectiveCarrierConfig resolves the effective carrier config for a
// PLMN, applying presets and runtime overrides.
func ResolveEffectiveCarrierConfig(mcc, mnc string) EffectiveCarrierConfig {
	cfg := GetGlobalDefaultConfig()
	cfg.MCC = strings.TrimSpace(mcc)
	cfg.MNC = strings.TrimSpace(mnc)

	// Apply the matching preset.
	for _, preset := range defaultPresets {
		if preset.MCC == cfg.MCC && preset.MNC == cfg.MNC {
			cfg.MergeFromPreset(preset)
			break
		}
	}
	// Apply the runtime override, if any.
	if ov, ok := carrierOverrideByKey(plmnKey(cfg.MCC, cfg.MNC)); ok {
		applyCarrierOverride(&cfg, ov)
	}
	// Fill defaults.
	if cfg.IMS.Domain == "" {
		cfg.IMS.Domain = DefaultCarrierIMSDomain(cfg.MCC, cfg.MNC)
	}
	if cfg.IMS.EPDGAddr == "" {
		cfg.IMS.EPDGAddr = DefaultCarrierStandardEPDGAddr()
	}
	cfg.IMS = NormalizeIMSRegisterTemplate(cfg.IMS)
	cfg.E911 = NormalizeE911Policy(cfg.E911)
	return cfg
}

// applyCarrierOverride applies an override to a config.
func applyCarrierOverride(cfg *EffectiveCarrierConfig, ov CarrierOverride) {
	if ov.PresetID != "" {
		cfg.PresetID = ov.PresetID
	}
	if ov.E911.Enabled {
		cfg.E911 = ov.E911
	}
	if ov.IMS.Domain != "" {
		cfg.IMS.Domain = ov.IMS.Domain
	}
	if ov.IMS.EPDGAddr != "" {
		cfg.IMS.EPDGAddr = ov.IMS.EPDGAddr
	}
	if ov.IMS.RegisterPolicy != "" {
		cfg.IMS.RegisterPolicy = ov.IMS.RegisterPolicy
	}
	if ov.IMS.Transport != "" {
		cfg.IMS.Transport = ov.IMS.Transport
	}
}

// --- runtime override store ---

var (
	overrideMu sync.RWMutex
	overrides  = map[string]CarrierOverride{}
)

// SetCarrierOverrides replaces the runtime override set.
func SetCarrierOverrides(ovs []CarrierOverride) {
	overrideMu.Lock()
	defer overrideMu.Unlock()
	overrides = make(map[string]CarrierOverride, len(ovs))
	for _, o := range ovs {
		overrides[plmnKey(o.MCC, o.MNC)] = NormalizeCarrierOverride(o)
	}
}

// ClearCarrierOverrides removes all runtime overrides.
func ClearCarrierOverrides() {
	overrideMu.Lock()
	defer overrideMu.Unlock()
	overrides = map[string]CarrierOverride{}
}

// carrierOverrideByKey returns the override for a PLMN key.
func carrierOverrideByKey(key string) (CarrierOverride, bool) {
	overrideMu.RLock()
	defer overrideMu.RUnlock()
	o, ok := overrides[key]
	return o, ok
}

// hasExternalCarrierOverrideKey reports whether an override exists for a key.
func hasExternalCarrierOverrideKey(key string) bool {
	_, ok := carrierOverrideByKey(key)
	return ok
}

// hasExternalCarrierOverrideRegisterPolicyKey reports whether an override with
// a register-policy exists for a key.
func hasExternalCarrierOverrideRegisterPolicyKey(key string) bool {
	o, ok := carrierOverrideByKey(key)
	return ok && o.IMS.RegisterPolicy != ""
}

// LoadCarrierOverridesFile loads overrides from a JSON file.
func LoadCarrierOverridesFile(path string) ([]CarrierOverride, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ovs []CarrierOverride
	if err := json.Unmarshal(data, &ovs); err != nil {
		return nil, fmt.Errorf("policy: parse overrides %s: %w", path, err)
	}
	for i := range ovs {
		ovs[i] = NormalizeCarrierOverride(ovs[i])
	}
	return ovs, nil
}

// LoadAndSetCarrierOverridesFile loads overrides from a file and installs them.
func LoadAndSetCarrierOverridesFile(path string) (int, error) {
	ovs, err := LoadCarrierOverridesFile(path)
	if err != nil {
		return 0, err
	}
	SetCarrierOverrides(ovs)
	return len(ovs), nil
}

// parsePLMNKey parses a "mcc-mnc" key.
func parsePLMNKey(key string) (mcc, mnc string) {
	parts := strings.SplitN(key, "-", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return key, ""
}

var _ = errors.New
