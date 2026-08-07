package runtimehost

import (
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
)

const (
	defaultIMSExpires         = 600000 * time.Second
	defaultIMSSupportedHeader = "path,sec-agree"
	defaultIMSAllowHeader     = "OPTIONS, REGISTER, SUBSCRIBE, NOTIFY, PUBLISH, INVITE, ACK, BYE, CANCEL, UPDATE, PRACK, REFER, INFO, MESSAGE"
	defaultIMSContactMode     = "android_default"
	defaultIMSAccessType      = "wlan1"
	defaultIMSICSIRef         = "urn%3Aurn-7%3A3gpp-service.ims.icsi.mmtel," +
		"urn%3Aurn-7%3A3gpp-service.ims.icsi.oma.cpm.msg," +
		"urn%3Aurn-7%3A3gpp-service.ims.icsi.oma.cpm.sms"
)

var (
	defaultIMSContactParamOrder = []string{
		"access_type", "sip_instance", "audio", "smsip", "icsi_ref",
	}
	giffgaffIMSContactParamOrder = []string{
		"access_type", "sip_instance", "audio", "smsip", "icsi_ref",
		"mid_call", "srvcc_alerting", "ps2cs_srvcc_orig_pre_alerting",
	}
)

func imsRegisterTemplateForProfile(profile identity.Profile) imscore.IMSRegisterTemplate {
	order := defaultIMSContactParamOrder
	if isGiffgaffPLMN(profile.MCC, profile.MNC) {
		order = giffgaffIMSContactParamOrder
	}
	return imscore.IMSRegisterTemplate{
		Expires:         defaultIMSExpires,
		SupportedHeader: defaultIMSSupportedHeader,
		AllowHeader:     defaultIMSAllowHeader,
		ContactMode:     defaultIMSContactMode,
		AccessType:      defaultIMSAccessType,
		ICSIRef:         defaultIMSICSIRef,
		ContactOrder:    append([]string(nil), order...),
	}
}

func isGiffgaffPLMN(mcc, mnc string) bool {
	mcc = strings.TrimSpace(mcc)
	mnc = strings.TrimLeft(strings.TrimSpace(mnc), "0")
	return mcc == "234" && mnc == "10"
}
