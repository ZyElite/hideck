package imscore

import (
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
)

const defaultIMSLocalPort = 5060

// BuildIMSConfigFromCarrier restores the original carrier-plan constructor.
// Its explicit string parameters are retained for source compatibility.
func BuildIMSConfigFromCarrier(
	deviceID, imsi, sipInstance, mcc, mnc, imsDomain, userAgent, localAddr string,
	plan policy.CarrierPlan,
) IMSConfig {
	template := policy.NormalizeIMSRegisterTemplate(plan.IMS.RegisterTemplate)
	domain := resolveCarrierIMSDomain(imsDomain, mcc, mnc, plan.IMS.Domain)
	impi, impu := derivedIMSIdentities(imsi, domain)
	localPort := plan.IMS.LocalPort
	if localPort < 1 {
		localPort = defaultIMSLocalPort
	}
	securityEnabled := policy.IMSRegisterTemplateSecAgreeMode(template) != "disabled"
	cfg := IMSConfig{
		Enabled: true, DeviceID: strings.TrimSpace(deviceID),
		PCSCF: strings.TrimSpace(plan.IMS.PCSCF), Registrar: strings.TrimSpace(plan.IMS.Registrar),
		Domain: domain, Realm: firstNonBlank(plan.IMS.Realm, domain), IMPI: impi, IMPU: impu,
		CarrierPresetID: strings.TrimSpace(plan.Metadata.PresetID), IMSRegisterTemplate: template,
		IMSRegisterPolicySource: strings.TrimSpace(plan.IMS.RegisterPolicySource),
		LocalAddr:               strings.TrimSpace(localAddr), LocalPort: localPort,
		Transport:           policy.NormalizeIMSTransport(plan.IMS.Transport),
		UserAgent:           firstNonBlank(userAgent, plan.IMS.UserAgent),
		PAccessNetworkInfo:  carrierPANI(imsi, sipInstance, deviceID, domain, mcc),
		CellularNetworkInfo: GenerateDefaultCellularNetworkInfo(mcc, mnc),
		SIPInstance:         strings.TrimSpace(sipInstance), IcsiRef: template.ICSIRef,
		TCPKeepaliveSeconds:        plan.IMS.TCPKeepaliveSeconds,
		OptionsPingIntervalSeconds: plan.IMS.OptionsPingIntervalSeconds,
		IMScore:                    IMScoreConfig{Enabled: true, ReceiverTransport: template.SMSReceiverTransport},
		EnableIPSec3GPP:            &securityEnabled,
		SMSRoutingMethod:           policy.NormalizeSMSRoutingMethod(plan.SMS.RoutingMethod),
		SMSRoutingGW:               strings.TrimSpace(plan.SMS.RoutingGW), ForceSMSCAuth: plan.SMS.ForceSMSCAuth,
		IMSI: strings.TrimSpace(imsi), IMEI: strings.TrimSpace(sipInstance),
	}
	cfg.syncCompatibilityFields()
	return cfg
}

func resolveCarrierIMSDomain(profileDomain, mcc, mnc, planDomain string) string {
	if domain := policy.NormalizeIMSDomain(profileDomain); domain != "" {
		return domain
	}
	if domain := policy.NormalizeIMSDomain(planDomain); domain != "" {
		return domain
	}
	if strings.TrimSpace(mcc) == "" || strings.TrimSpace(mnc) == "" {
		return ""
	}
	return policy.DefaultCarrierIMSDomain(strings.TrimSpace(mcc), strings.TrimSpace(mnc))
}

func derivedIMSIdentities(imsi, domain string) (string, string) {
	imsi, domain = strings.TrimSpace(imsi), strings.TrimSpace(domain)
	if imsi == "" || domain == "" {
		return "", ""
	}
	return fmt.Sprintf("%s@%s", imsi, domain), fmt.Sprintf("sip:%s@%s", imsi, domain)
}

func carrierPANI(imsi, sipInstance, deviceID, domain, mcc string) string {
	seed := stablePANIGenerationSeed([]string{imsi, sipInstance, deviceID, domain})
	return AppendPAccessNetworkCountry(GenerateStablePAccessNetworkInfo(seed), CountryISO2FromMCC(mcc))
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
