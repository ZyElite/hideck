package runtimecore

import (
	"context"
	"errors"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
)

func preparedSessionWithRuntimeOverride(
	prepared profile.PreparedSession,
	override string,
) profile.PreparedSession {
	override = strings.TrimSpace(override)
	if override == "" {
		return prepared
	}
	prepared.EPDGAddr = override
	prepared.EPDGSource = "runtime_override"
	prepared.CarrierPlan.EPDG.Addr = override
	prepared.CarrierPlan.EPDG.AddrSource = "runtime_override"
	return prepared
}

func PrepareSessionStart(
	ctx context.Context,
	req RuntimeStartRequest,
) (profile.PreparedSession, error) {
	if err := contextError(ctx); err != nil {
		return profile.PreparedSession{}, err
	}
	if req.Prepared != nil {
		prepared := clonePreparedSession(*req.Prepared)
		return validatePrepared(preparedSessionWithRuntimeOverride(prepared, req.RuntimeEPDGOverride))
	}
	value, err := profile.Build(req.Profile, "")
	if err != nil {
		return profile.PreparedSession{}, err
	}
	carrierPlan := policy.CarrierPlanFromEffectiveConfig(
		policy.ResolveEffectiveCarrierConfig(value.MCC, value.MNC),
	)
	value.IMEI, _ = profile.ResolveIdentityIMEI(
		value.IMSI, value.IMEI, value.UserAgent, carrierPlan,
	)
	identity, err := resolveIMSIdentity(req, carrierPlan)
	if err != nil {
		return profile.PreparedSession{}, err
	}
	prepared := profile.PreparedSession{
		Profile:            value,
		CarrierPlan:        carrierPlan,
		IMSIdentity:        identity,
		AuthPlan:           resolveAuthPlan(req, identity),
		EPDGAddr:           carrierPlan.EPDG.Addr,
		EPDGSource:         carrierPlan.EPDG.AddrSource,
		IdentityIMEISource: resolveIMEISource(value, carrierPlan),
	}
	return validatePrepared(preparedSessionWithRuntimeOverride(prepared, req.RuntimeEPDGOverride))
}

func resolveIMSIdentity(
	req RuntimeStartRequest,
	carrierPlan policy.CarrierPlan,
) (profile.IMSIdentityResult, error) {
	identity := req.IMSIdentity
	if identity.IMPI != "" || identity.IMPU != "" || identity.Domain != "" {
		return identity, nil
	}
	provider := identityProvider(req)
	if provider == nil {
		return derivedIdentity(req.Profile, carrierPlan), nil
	}
	resolved, err := provider.GetISIMIdentity()
	if err != nil {
		return profile.IMSIdentityResult{}, err
	}
	result := profile.IMSIdentityResult{
		RequestedSource: carrierPlan.IMS.IdentitySource,
		ActualSource:    "isim",
		Applied:         true,
		IMPI:            strings.TrimSpace(resolved.IMPI),
		Domain:          strings.TrimSpace(resolved.Domain),
	}
	if len(resolved.IMPU) > 0 {
		result.IMPU = strings.TrimSpace(resolved.IMPU[0])
	}
	return result, nil
}

func identityProvider(req RuntimeStartRequest) profile.Provider {
	if req.SIM != nil {
		if provider := req.SIM.IMSIdentityProvider(); provider != nil {
			return provider
		}
	}
	if req.Access != nil {
		return req.Access.IMSIdentityProvider()
	}
	return nil
}

func derivedIdentity(value profile.Profile, carrierPlan policy.CarrierPlan) profile.IMSIdentityResult {
	domain := strings.TrimSpace(carrierPlan.IMS.Domain)
	if domain == "" {
		domain = value.IMSDomain
	}
	impi := value.IMSI + "@" + domain
	return profile.IMSIdentityResult{
		RequestedSource: carrierPlan.IMS.IdentitySource,
		ActualSource:    "derived",
		Applied:         true,
		IMPI:            impi,
		IMPU:            "sip:" + impi,
		Domain:          domain,
	}
}

func resolveAuthPlan(req RuntimeStartRequest, identity profile.IMSIdentityResult) profile.AuthPlan {
	imsApp := profile.NormalizeAKAApp(identity.AKAAppPreference)
	if imsApp == "" {
		imsApp = profile.AKAAppUSIM
	}
	plan := profile.NewAuthPlan(profile.AKAAppUSIM, imsApp)
	if req.Access != nil {
		caps := req.Access.Capabilities()
		plan.ISIMAvailable = caps.ISIMAKA
		plan.USIMAvailable = caps.SIM
	}
	return plan.Normalize()
}

func resolveIMEISource(value profile.Profile, plan policy.CarrierPlan) string {
	_, source := profile.ResolveIdentityIMEI(value.IMSI, value.IMEI, value.UserAgent, plan)
	return source
}

func validatePrepared(prepared profile.PreparedSession) (profile.PreparedSession, error) {
	if strings.TrimSpace(prepared.Profile.IMSI) == "" {
		return profile.PreparedSession{}, errors.New("runtimecore: prepared session has no IMSI")
	}
	if strings.TrimSpace(prepared.EPDGAddr) == "" {
		return profile.PreparedSession{}, errors.New("runtimecore: prepared session has no ePDG address")
	}
	return clonePreparedSession(prepared), nil
}

func clonePreparedSession(prepared profile.PreparedSession) profile.PreparedSession {
	prepared.CarrierPlan.IKE.IKEProposals = append([]string(nil), prepared.CarrierPlan.IKE.IKEProposals...)
	prepared.CarrierPlan.IKE.ESPProposals = append([]string(nil), prepared.CarrierPlan.IKE.ESPProposals...)
	prepared.CarrierPlan.IKE.AllowedLegacyCiphers = append([]string(nil), prepared.CarrierPlan.IKE.AllowedLegacyCiphers...)
	return prepared
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
