package imscore

import (
	"errors"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
)

type registerSecurityChallenge struct {
	server   *securityMechanism
	verify   string
	decision secAgreeDecision
}

func decideSecAgreeAfterChallenge(
	cfg *IMSConfig,
	template policy.IMSRegisterTemplate,
	usableOffer bool,
) secAgreeDecision {
	switch effectiveSecAgreeMode(cfg, template) {
	case "disabled":
		return secAgreeDecision{mode: securityModePlain, reason: securityDisabled}
	case "required":
		if !usableOffer {
			return secAgreeDecision{mode: securityModeIPSec, reason: securityRequired, err: errMissingUsableSecurityServer}
		}
		return secAgreeDecision{useIPSec: true, mode: securityModeIPSec, reason: securitySelected}
	default:
		if usableOffer {
			return secAgreeDecision{useIPSec: true, mode: securityModeIPSec, reason: securitySelected}
		}
		return secAgreeDecision{mode: securityModePlain, reason: securityAutoFallback}
	}
}

func decideInitialRegisterSuccessSecurity(
	cfg *IMSConfig,
	template policy.IMSRegisterTemplate,
	hasSecurityServer bool,
) error {
	if effectiveSecAgreeMode(cfg, template) != "required" {
		return nil
	}
	if hasSecurityServer {
		return errors.New("initial_200_security_server_without_ipsec_install_required")
	}
	return errors.New("missing_usable_security_server_offer_initial_200")
}

func (s *Service) evaluateRegisterSecurityChallenge(
	response *sipResponse,
	template policy.IMSRegisterTemplate,
) registerSecurityChallenge {
	server, verify, err := selectSecurityServerForAuth(response, template)
	return registerSecurityChallenge{
		server: server, verify: verify,
		decision: decideSecAgreeAfterChallenge(s.cfg, template, err == nil),
	}
}

func selectSecurityServerForAuth(
	response *sipResponse,
	template policy.IMSRegisterTemplate,
) (*securityMechanism, string, error) {
	if response == nil {
		return selectSecurityServerOfferForTemplate("", template)
	}
	return selectSecurityServerOfferForTemplate(response.Header("Security-Server"), template)
}

func updateAuthResponseRouteSecurity(session *registerSession, response *sipResponse) {
	if session == nil || response == nil {
		return
	}
	if path := strings.TrimSpace(response.Header("Path")); path != "" {
		session.path = path
	}
}

func classifySecurityFallbackReason(decision secAgreeDecision) string {
	if decision.reason != "" {
		return decision.reason
	}
	if decision.useIPSec {
		return securitySelected
	}
	return ""
}
