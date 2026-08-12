package policy

import (
	"fmt"

	"github.com/iniwex5/vowifi-go/internal/vowifi/common"
)

const (
	defaultDNS                   = "1.1.1.1:53"
	defaultIMSExpires            = 600000
	defaultTemporaryRetrySeconds = 60
	defaultSupportedHeader       = "path,sec-agree"
	defaultAllowHeader           = "OPTIONS, REGISTER, SUBSCRIBE, NOTIFY, PUBLISH, INVITE, ACK, BYE, CANCEL, UPDATE, PRACK, REFER, INFO, MESSAGE"
	defaultVoiceSupportedHeader  = "100rel, timer, replaces, norefersub, early-session"
	defaultVoiceAllowHeader      = "INVITE, ACK, CANCEL, BYE, UPDATE, REFER, NOTIFY, MESSAGE, OPTIONS"
	defaultVoiceAcceptContact    = `*;+g.3gpp.icsi-ref="urn%3Aurn-7%3A3gpp-service.ims.icsi.mmtel"`
	defaultVoicePreferredService = "urn:urn-7:3gpp-service.ims.icsi.mmtel"
	defaultICSIRef               = "urn%3Aurn-7%3A3gpp-service.ims.icsi.mmtel," +
		"urn%3Aurn-7%3A3gpp-service.ims.icsi.sms," +
		"urn%3Aurn-7%3A3gpp-service.ims.icsi.oma.cpm.msg," +
		"urn%3Aurn-7%3A3gpp-service.ims.icsi.oma.cpm.sms"
)

var (
	defaultTemporaryStatusCodes = []int{408, 429, 500, 502, 503, 504}
	defaultForbiddenStatusCodes = []int{403}
	defaultFallbackStatusCodes  = []int{400, 403, 500}
	defaultContactParams        = []string{"access_type", "sip_instance", "audio", "smsip", "icsi_ref"}
	defaultSecurityMechanisms   = []IPSec3GPPSecurityMechanism{
		{Alg: "hmac-md5-96", EAlg: "des-ede3-cbc", Prot: "esp", Mode: "trans"},
		{Alg: "hmac-md5-96", EAlg: "aes-cbc", Prot: "esp", Mode: "trans"},
		{Alg: "hmac-md5-96", EAlg: "null", Prot: "esp", Mode: "trans"},
		{Alg: "hmac-sha-1-96", EAlg: "des-ede3-cbc", Prot: "esp", Mode: "trans"},
		{Alg: "hmac-sha-1-96", EAlg: "aes-cbc", Prot: "esp", Mode: "trans"},
		{Alg: "hmac-sha-1-96", EAlg: "null", Prot: "esp", Mode: "trans"},
	}
)

func DefaultIMSRegisterPolicy() IMSRegisterPolicy {
	return IMSRegisterPolicy{
		ID:                               "default",
		TemporaryStatusCodes:             cloneInts(defaultTemporaryStatusCodes),
		ForbiddenStatusCodes:             cloneInts(defaultForbiddenStatusCodes),
		InitialRejectFallbackStatusCodes: cloneInts(defaultFallbackStatusCodes),
		TemporaryRetrySeconds:            defaultTemporaryRetrySeconds,
	}
}

func DefaultIMSRegisterTemplate() IMSRegisterTemplate {
	return IMSRegisterTemplate{
		ID: "defaultIMSRegisterTemplate", UsePlainDigestPlaceholder: true,
		Expires: defaultIMSExpires, ContactMode: "android_default",
		SupportedHeader: defaultSupportedHeader, AllowHeader: defaultAllowHeader,
		AccessType: "wlan1", ICSIRef: defaultICSIRef,
		ContactParamOrder:    cloneStrings(defaultContactParams),
		VoiceSupportedHeader: defaultVoiceSupportedHeader, VoiceAllowHeader: defaultVoiceAllowHeader,
		VoiceAcceptContact: defaultVoiceAcceptContact, VoicePPreferredService: defaultVoicePreferredService,
		IncludePANIAuthenticated: true, SecAgreeMode: "auto",
		SecurityClientIncludesServerParams: true,
		SecurityClientMechanisms:           cloneMechanisms(defaultSecurityMechanisms),
		EnableInitialRejectFallback:        true, RegisterPolicy: DefaultIMSRegisterPolicy(),
	}
}

func DefaultCarrierStandardEPDGAddr(mcc, mnc string) string {
	return fmt.Sprintf("epdg.epc.mnc%s.mcc%s.pub.3gppnetwork.org", common.Plmn3(mnc), common.Plmn3(mcc))
}

func DefaultCarrierIMSDomain(mcc, mnc string) string {
	return fmt.Sprintf("ims.mnc%s.mcc%s.3gppnetwork.org", common.Plmn3(mnc), common.Plmn3(mcc))
}

func DefaultCarrierIKEProposals() []string {
	return []string{
		"aes128-sha256-modp2048", "aes128-sha256-modp1024",
		"aes128-sha1-modp1024", "aes256-sha1-modp1024",
	}
}

func DefaultCarrierESPProposals() []string {
	return []string{"aes256gcm16", "aes128gcm16", "aes128-sha256", "aes128-sha1"}
}

func cloneStrings(values []string) []string { return append([]string(nil), values...) }
func cloneInts(values []int) []int          { return append([]int(nil), values...) }

func cloneMechanisms(values []IPSec3GPPSecurityMechanism) []IPSec3GPPSecurityMechanism {
	return append([]IPSec3GPPSecurityMechanism(nil), values...)
}
