package profile

import (
	"errors"
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/common"
	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
)

func Normalize(value Profile) Profile {
	return Profile{
		IMSI:      strings.TrimSpace(value.IMSI),
		MCC:       strings.TrimSpace(value.MCC),
		MNC:       strings.TrimSpace(value.MNC),
		IMEI:      strings.TrimSpace(value.IMEI),
		UserAgent: strings.TrimSpace(value.UserAgent),
		SMSC:      strings.TrimSpace(value.SMSC),
		IMSDomain: policy.NormalizeIMSDomain(value.IMSDomain),
	}
}

func Build(value Profile, fallbackUserAgent string) (Profile, error) {
	result := Normalize(value)
	if result.IMSI == "" {
		return Profile{}, errors.New("无法获取 IMSI")
	}
	if result.MCC == "" || result.MNC == "" {
		return Profile{}, fmt.Errorf("缺少 SIM 归属 MCC/MNC: %s", result.IMSI)
	}
	if result.UserAgent == "" {
		result.UserAgent = strings.TrimSpace(fallbackUserAgent)
	}
	if result.UserAgent == "" {
		result.UserAgent = ResolveUserAgentForModel("")
	}
	if result.IMSDomain == "" {
		result.IMSDomain = fmt.Sprintf(
			"ims.mnc%s.mcc%s.3gppnetwork.org",
			common.Plmn3(result.MNC), common.Plmn3(result.MCC),
		)
	}
	return Normalize(result), nil
}

func ResolveIdentityIMEI(
	imsi string,
	inputIMEI string,
	userAgent string,
	carrierPlan policy.CarrierPlan,
) (string, string) {
	if value := GenerateStableIMEIForModel(imsi, carrierPlan.Device.Model); value != "" {
		return value, "carrier_device_model"
	}
	if value := strings.TrimSpace(carrierPlan.Device.IdentityIMEI); value != "" {
		return value, "device_identity_imei"
	}
	if value := strings.TrimSpace(inputIMEI); value != "" {
		return value, "input"
	}
	if value := GenerateStableIMEIForModel(imsi, userAgent); value != "" {
		return value, "user_agent"
	}
	return "", ""
}
