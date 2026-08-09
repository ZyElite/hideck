package policy

import (
	"errors"
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/common"
)

var ErrVoWiFiPolicyBlocked = errors.New("vowifi blocked by policy")

var blockedMCCs = map[string]struct{}{"460": {}, "461": {}}

type VoWiFiPolicyBlockError struct{ MCC string }

func (err *VoWiFiPolicyBlockError) Error() string {
	if err == nil || strings.TrimSpace(err.MCC) == "" {
		return "VoWiFi 被运营商策略拒绝"
	}
	return fmt.Sprintf("VoWiFi 被运营商策略拒绝: mcc=%s", err.MCC)
}

func (err *VoWiFiPolicyBlockError) Unwrap() error { return ErrVoWiFiPolicyBlocked }

func IsVoWiFiBlockedMCC(mcc string) bool {
	mcc, ok := normalizeMCC(mcc)
	if !ok {
		return false
	}
	_, blocked := blockedMCCs[mcc]
	return blocked
}

func NewVoWiFiBlockedMCCError(mcc string) error {
	normalized, ok := normalizeMCC(mcc)
	if !ok {
		normalized = strings.TrimSpace(mcc)
	}
	return &VoWiFiPolicyBlockError{MCC: normalized}
}

func IsVoWiFiPolicyBlockedError(err error) bool { return errors.Is(err, ErrVoWiFiPolicyBlocked) }

func normalizeMCC(mcc string) (string, bool) {
	mcc = common.Plmn3(strings.TrimSpace(mcc))
	return mcc, len(mcc) == 3 && isDigits(mcc)
}
