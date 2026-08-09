package runtimecore

import (
	"context"
	"errors"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
	"github.com/iniwex5/vowifi-go/internal/vowifi/startup"
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
	prepared.EPDGSource = "redirect"
	prepared.CarrierPlan.EPDG.Addr = override
	prepared.CarrierPlan.EPDG.AddrSource = "redirect"
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
	var provider profile.Provider
	if req.SIM != nil {
		provider = req.SIM.IMSIdentityProvider()
	}
	return startup.PrepareStart(
		req.DeviceID, req.Profile, req.RuntimeEPDGOverride,
		req.IMSIdentity, provider, req.Access,
	)
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
