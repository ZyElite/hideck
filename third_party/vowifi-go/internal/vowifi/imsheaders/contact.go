// Package imsheaders builds the IMS-specific SIP headers used by registration
// and dialog flows.
package imsheaders

import (
	"fmt"
	"strings"
)

const defaultICSIRef = "urn%3Aurn-7%3A3gpp-service.ims.icsi.mmtel," +
	"urn%3Aurn-7%3A3gpp-service.ims.icsi.oma.cpm.msg," +
	"urn%3Aurn-7%3A3gpp-service.ims.icsi.oma.cpm.sms"

// ContactOptions contains the endpoint and feature values used to build an
// IMS Contact URI and its ordered parameters.
type ContactOptions struct {
	ContactID  string
	LocalAddr  string
	LocalPortC int
	LocalPortS int

	AccessType        string
	ContactParamOrder []string
	SIPInstance       string
	IcsiRef           string
	IMEI              string

	// Current additive field spellings remain source-compatible.
	ParamOrder []string
	Instance   string
	ICSIRef    string
}

// ContactParam is one structured Contact header parameter.
type ContactParam struct {
	Name  string
	Value string
}

// ContactPort returns the protected server port when present, otherwise the
// client port.
func ContactPort(options ContactOptions) int {
	if options.LocalPortS > 0 {
		return options.LocalPortS
	}
	return options.LocalPortC
}

func baseContactURI(options ContactOptions) string {
	return fmt.Sprintf("sip:%s@%s", strings.TrimSpace(options.ContactID), formatHostForSIP(options.LocalAddr))
}

// ContactURI builds the registration Contact URI with its transport parameter.
func ContactURI(options ContactOptions, transport string) string {
	return ContactURIWithOptions(options, transport, true)
}

func formatHostForSIP(host string) string {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

func sipInstanceIMEIDigits(imei string) string {
	var digits strings.Builder
	digits.Grow(len(imei))
	for _, character := range imei {
		if character >= '0' && character <= '9' {
			digits.WriteRune(character)
			continue
		}
		if character != '-' {
			return ""
		}
	}
	return digits.String()
}

// NormalizeSipInstance normalizes an IMEI into the GSMA SIP instance URN.
func NormalizeSipInstance(instance string) string {
	instance = strings.TrimSpace(instance)
	if instance == "" {
		return ""
	}
	if strings.HasPrefix(instance, "<") && strings.HasSuffix(instance, ">") {
		return instance
	}
	if strings.HasPrefix(instance, "urn:gsma:imei:") {
		return "<" + instance + ">"
	}
	digits := sipInstanceIMEIDigits(instance)
	if len(digits) != 14 && len(digits) != 15 {
		return "<" + instance + ">"
	}
	if len(digits) == 14 {
		digits += string(rune('0' + imeiCheckDigit(digits)))
	}
	return fmt.Sprintf("<urn:gsma:imei:%s-%s-%s>", digits[:8], digits[8:14], digits[14:])
}

func imeiCheckDigit(digits string) rune {
	sum := 0
	double := true
	for index := len(digits) - 1; index >= 0; index-- {
		value := int(digits[index] - '0')
		if double {
			value *= 2
			if value > 9 {
				value -= 9
			}
		}
		sum += value
		double = !double
	}
	return rune((10 - sum%10) % 10)
}

func icsiRefValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultICSIRef
	}
	if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		return value
	}
	return fmt.Sprintf("%q", value)
}

// ContactURIWithOptions builds the Contact URI, including the selected port
// and optional transport URI parameter.
func ContactURIWithOptions(options ContactOptions, transport string, includeTransport bool) string {
	transport = strings.ToLower(strings.TrimSpace(transport))
	if transport == "" {
		transport = "tcp"
	}
	uri := baseContactURI(options)
	if port := ContactPort(options); port > 0 {
		uri += fmt.Sprintf(":%d", port)
	}
	if includeTransport {
		uri += ";transport=" + transport
	}
	return uri
}

// ContactParams returns Contact parameters in the carrier-provided order.
func ContactParams(options ContactOptions) []ContactParam {
	instance := NormalizeSipInstance(strings.TrimSpace(contactSIPInstance(options)))
	if instance == "" {
		instance = NormalizeSipInstance(options.IMEI)
	}
	values := contactParamValues{
		accessType: strings.TrimSpace(options.AccessType),
		instance:   instance,
		icsiRef:    icsiRefValue(contactICSIRef(options)),
	}
	order := contactParamOrder(options)
	params := make([]ContactParam, 0, len(order))
	for _, name := range order {
		if param, ok := contactParam(name, values); ok {
			params = append(params, param)
		}
	}
	return params
}

func contactParamOrder(options ContactOptions) []string {
	if options.ContactParamOrder != nil {
		return options.ContactParamOrder
	}
	return options.ParamOrder
}

func contactSIPInstance(options ContactOptions) string {
	if options.SIPInstance != "" {
		return options.SIPInstance
	}
	return options.Instance
}

func contactICSIRef(options ContactOptions) string {
	if options.IcsiRef != "" {
		return options.IcsiRef
	}
	return options.ICSIRef
}

type contactParamValues struct {
	accessType string
	instance   string
	icsiRef    string
}

func contactParam(name string, values contactParamValues) (ContactParam, bool) {
	switch name {
	case "audio":
		return ContactParam{Name: "audio"}, true
	case "smsip":
		return ContactParam{Name: "+g.3gpp.smsip"}, true
	case "reg_id":
		return ContactParam{Name: "reg-id", Value: "1"}, true
	case "icsi_ref":
		return ContactParam{Name: "+g.3gpp.icsi-ref", Value: values.icsiRef}, true
	case "mid_call":
		return ContactParam{Name: "+g.3gpp.mid-call"}, true
	case "access_type":
		return ContactParam{Name: "+g.3gpp.accesstype", Value: fmt.Sprintf("%q", values.accessType)}, true
	case "sip_instance":
		return ContactParam{Name: "+sip.instance", Value: fmt.Sprintf("%q", values.instance)}, true
	case "srvcc_alerting":
		return ContactParam{Name: "+g.3gpp.srvcc-alerting"}, true
	case "ps2cs_srvcc_orig_pre_alerting":
		return ContactParam{Name: "+g.3gpp.ps2cs-srvcc-orig-pre-alerting"}, true
	default:
		return ContactParam{}, false
	}
}

// IMSContactOptions is the additive raw-URI compatibility surface.
type IMSContactOptions struct {
	Transport  string
	AccessType string
	Instance   string
	ICSIRef    string
	ParamOrder []string
}

// IMSContactURI serializes a raw SIP URI with recovered Contact parameters.
func IMSContactURI(uri string, options IMSContactOptions) string {
	uri = strings.TrimSpace(uri)
	if transport := strings.ToLower(strings.TrimSpace(options.Transport)); transport != "" {
		uri += ";transport=" + transport
	}
	params := ContactParams(ContactOptions{
		AccessType: options.AccessType, Instance: options.Instance,
		ICSIRef: options.ICSIRef, ParamOrder: options.ParamOrder,
	})
	return serializeContact(uri, params)
}

func serializeContact(uri string, params []ContactParam) string {
	var builder strings.Builder
	builder.WriteByte('<')
	builder.WriteString(uri)
	builder.WriteByte('>')
	for _, param := range params {
		builder.WriteByte(';')
		builder.WriteString(param.Name)
		if param.Value != "" {
			builder.WriteByte('=')
			builder.WriteString(param.Value)
		}
	}
	return builder.String()
}
