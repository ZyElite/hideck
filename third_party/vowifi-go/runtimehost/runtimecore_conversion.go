package runtimehost

import (
	"github.com/iniwex5/vowifi-go/internal/runtimehostcarrier"
	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
	internalprofile "github.com/iniwex5/vowifi-go/internal/vowifi/profile"
	"github.com/iniwex5/vowifi-go/internal/vowifi/runtimecore"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
)

func sessionConfigFromInternal(config runtimecore.SessionConfig) SessionConfig {
	return SessionConfig{
		Ctx: config.Ctx, DeviceID: config.DeviceID, TraceID: config.TraceID,
		Prepared:      preparedSessionFromInternal(config.Prepared),
		DataplaneMode: config.DataplaneMode, TUNName: config.TUNName,
		Proxy: proxyFromInternal(config.Proxy), DNSServer: config.DNSServer,
	}
}

func authPlanToInternal(value identity.AuthPlan) internalprofile.AuthPlan {
	return internalprofile.AuthPlan{
		EPDGApp: internalprofile.NormalizeAKAApp(value.EPDGApp),
		IMSApp:  internalprofile.NormalizeAKAApp(value.IMSApp),
	}
}

func preparedSessionFromInternal(value internalprofile.PreparedSession) identity.PreparedSession {
	carrierConfig := runtimehostcarrier.FromInternal(
		policy.EffectiveCarrierConfigFromCarrierPlan(value.CarrierPlan),
	)
	return identity.PreparedSession{
		Profile: identity.Profile{
			IMSI: value.Profile.IMSI, MCC: value.Profile.MCC, MNC: value.Profile.MNC,
			IMEI: value.Profile.IMEI, UserAgent: value.Profile.UserAgent,
			SMSC: value.Profile.SMSC, IMSDomain: value.Profile.IMSDomain,
		},
		EffectiveCarrier: carrierConfig,
		IMSIdentity: identity.IMSIdentityResult{
			RequestedSource:  value.IMSIdentity.RequestedSource,
			ActualSource:     value.IMSIdentity.ActualSource,
			AKAAppPreference: value.IMSIdentity.AKAAppPreference,
			Applied:          value.IMSIdentity.Applied, IMPI: value.IMSIdentity.IMPI,
			IMPU: value.IMSIdentity.IMPU, Domain: value.IMSIdentity.Domain,
		},
		AuthPlan: identity.AuthPlan{
			EPDGApp: value.AuthPlan.EPDGApp, IMSApp: value.AuthPlan.IMSApp,
		},
		EPDGAddr: value.EPDGAddr, EPDGSource: value.EPDGSource,
		IdentityIMEISource: value.IdentityIMEISource, CarrierConfig: carrierConfig,
	}
}

func preparedSessionPtrToInternal(value *identity.PreparedSession) *internalprofile.PreparedSession {
	if value == nil {
		return nil
	}
	carrierConfig := runtimehostcarrier.ToInternal(value.ResolvedCarrierConfig())
	return &internalprofile.PreparedSession{
		Profile: internalprofile.Profile{
			IMSI: value.Profile.IMSI, MCC: value.Profile.MCC, MNC: value.Profile.MNC,
			IMEI: value.Profile.IMEI, UserAgent: value.Profile.UserAgent,
			SMSC: value.Profile.SMSC, IMSDomain: value.Profile.IMSDomain,
		},
		CarrierPlan: policy.CarrierPlanFromEffectiveConfig(carrierConfig),
		IMSIdentity: internalprofile.IMSIdentityResult{
			RequestedSource:  value.IMSIdentity.RequestedSource,
			ActualSource:     value.IMSIdentity.ActualSource,
			AKAAppPreference: value.IMSIdentity.AKAAppPreference,
			Applied:          value.IMSIdentity.Applied,
			IMPI:             value.IMSIdentity.IMPI,
			IMPU:             value.IMSIdentity.IMPU,
			Domain:           value.IMSIdentity.Domain,
		},
		AuthPlan: authPlanToInternal(value.AuthPlan),
		EPDGAddr: value.EPDGAddr, EPDGSource: value.EPDGSource,
		IdentityIMEISource: value.IdentityIMEISource,
	}
}

func proxyFromInternal(proxy *runtimecore.ProxyConfig) *ProxyConfig {
	if proxy == nil {
		return nil
	}
	return &ProxyConfig{
		ID: proxy.ID, Addr: proxy.Addr, Username: proxy.Username,
		Password: proxy.Password, Enabled: proxy.Enabled,
	}
}
