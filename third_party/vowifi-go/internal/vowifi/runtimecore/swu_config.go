package runtimecore

import (
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/engine/ipsec"
	"github.com/iniwex5/vowifi-go/engine/swu"
)

func BuildSWUConfig(cfg SessionConfig) *swu.Config {
	prepared := cfg.Prepared
	plan := prepared.CarrierPlan
	dnsServer := strings.TrimSpace(cfg.DNSServer)
	if dnsServer == "" {
		dnsServer = plan.EPDG.DNSServer
	}
	aka := swu.AKAProvider(unsupportedSWUAKAProvider{message: "SWu AKA provider is unavailable"})
	if cfg.SIM != nil {
		aka = cfg.SIM.EPDGSIMProvider(prepared.EffectiveAuthPlan())
		if aka == nil {
			aka = unsupportedSWUAKAProvider{message: "SWu AKA provider is unavailable"}
		}
	}
	result := &swu.Config{
		DeviceID:                  strings.TrimSpace(cfg.DeviceID),
		DNSServer:                 dnsServer,
		EPDGAddr:                  strings.TrimSpace(prepared.EPDGAddr),
		EpDGAddr:                  strings.TrimSpace(prepared.EPDGAddr),
		EpDGPort:                  plan.EPDG.Port,
		APN:                       plan.EPDG.APN,
		IMSI:                      prepared.Profile.IMSI,
		MCC:                       prepared.Profile.MCC,
		MNC:                       prepared.Profile.MNC,
		SIM:                       aka,
		IPStackType:               plan.EPDG.IPStackType,
		IPStack:                   plan.EPDG.IPStackType,
		EnableDeviceIdentitySpoof: plan.Device.IdentityEnabled,
		DeviceIdentityIMEI:        firstNonEmpty(plan.Device.IdentityIMEI, prepared.Profile.IMEI),
		IKEIdentityMode:           plan.IKE.IKEIdentityMode,
		AKAChallengeMode:          plan.IKE.AKAChallengeMode,
		AKAIdentityMode:           plan.IKE.AKAIdentityMode,
		AlgorithmPolicy:           plan.IKE.AlgorithmPolicy,
		IKEProposals:              append([]string(nil), plan.IKE.IKEProposals...),
		ESPProposals:              append([]string(nil), plan.IKE.ESPProposals...),
		EnableLegacyCiphers:       plan.IKE.EnableLegacyCiphers,
		AllowedLegacyCiphers:      append([]string(nil), plan.IKE.AllowedLegacyCiphers...),
		DataplaneMode:             strings.TrimSpace(cfg.DataplaneMode),
		TUNName:                   strings.TrimSpace(cfg.TUNName),
		NATKeepaliveSeconds:       plan.IKE.NATKeepaliveSeconds,
		DPDIntervalSeconds:        plan.IKE.DPDIntervalSeconds,
		ReauthInterval:            plan.IKE.ReauthIntervalSeconds,
		ResumeTicket:              append([]byte(nil), cfg.ResumeTicket...),
		ResumeOldSKd:              append([]byte(nil), cfg.ResumeOldSKd...),
		OnTicketUpdate:            cfg.OnTicketUpdate,
		FastReauthID:              cfg.FastReauthID,
		FastReauthMK:              append([]byte(nil), cfg.FastReauthMK...),
		FastReauthKAut:            append([]byte(nil), cfg.FastReauthKAut...),
		FastReauthKEncr:           append([]byte(nil), cfg.FastReauthKEncr...),
		OnFastReauthUpdate:        cfg.OnFastReauthUpdate,
		OnProgress:                cfg.OnProgress,
	}
	result.AKAProvider = result.SIM
	if result.ReauthInterval > 0 {
		result.ReauthSeconds = time.Duration(result.ReauthInterval) * time.Second
	}
	applyProxy(result, cfg.Proxy)
	return result
}

func applyProxy(cfg *swu.Config, proxy *ProxyConfig) {
	if proxy == nil || !proxy.Enabled {
		return
	}
	cfg.ProxyAddr = strings.TrimSpace(proxy.Addr)
	cfg.Socks5Addr = cfg.ProxyAddr
	cfg.Socks5Username = proxy.Username
	cfg.Socks5Password = proxy.Password
	cfg.Proxy = &ipsec.Socks5Config{Username: proxy.Username, Password: proxy.Password}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
