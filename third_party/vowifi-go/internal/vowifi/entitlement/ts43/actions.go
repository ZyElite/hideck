package ts43

import "strings"

// BuildAuthAction keeps the original positional contract used by the AT&T
// provider. Several identity fields are intentionally accepted because they
// are part of that contract, even though only the authentication fields are
// placed in this action.
func BuildAuthAction(
	requestID int,
	imsi, _ string,
	imei, mcc, mnc string,
	_, _ string,
	token, _ string,
) AuthAction {
	return AuthAction{
		AuthType:     AuthTypeEAPAKA,
		ActionName:   ActionGetAuthentication,
		SubscriberID: BuildSubscriberID(imsi, mcc, mnc),
		RequestID:    requestID,
		UniqueID:     strings.TrimSpace(imei),
		Token:        strings.TrimSpace(token),
	}
}

func BuildEntitlementAction(requestID int) EntitlementAction {
	return EntitlementAction{
		EntitlementNames: []string{EntitlementNameVoWiFi},
		ActionName:       ActionGetEntitlement,
		RequestID:        requestID,
	}
}
