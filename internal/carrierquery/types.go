package carrierquery

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type Transport string

const (
	TransportSMS         Transport = "sms"
	TransportUSSD        Transport = "ussd"
	TransportUnsupported Transport = "unsupported"
)

type ResponseMode string

const (
	ResponseDirect ResponseMode = "direct"
	ResponseSMS    ResponseMode = "sms"
	ResponseNone   ResponseMode = "none"
)

type Rule struct {
	ID              string       `json:"id"`
	MCC             string       `json:"mcc"`
	MNC             string       `json:"mnc"`
	SPN             string       `json:"spn,omitempty"`
	Variant         string       `json:"variant,omitempty"`
	Operator        string       `json:"operator"`
	Transport       Transport    `json:"transport"`
	Destination     string       `json:"destination,omitempty"`
	Payload         string       `json:"payload,omitempty"`
	ResponseMode    ResponseMode `json:"response_mode"`
	ExpectedSenders []string     `json:"expected_senders,omitempty"`
	ParserPattern   string       `json:"parser_pattern,omitempty"`
	Currency        string       `json:"currency,omitempty"`
	CostStatus      string       `json:"cost_status"`
	EvidenceType    string       `json:"evidence_type"`
	EvidenceURL     string       `json:"evidence_url,omitempty"`
	Limitations     []string     `json:"limitations,omitempty"`
	Alternative     string       `json:"alternative,omitempty"`
	Enabled         bool         `json:"enabled"`
	BuiltIn         bool         `json:"built_in"`
}

var ruleIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func (r Rule) Validate() error {
	if !ruleIDPattern.MatchString(strings.TrimSpace(r.ID)) {
		return errors.New("规则 ID 只能包含字母、数字、点、下划线和连字符")
	}
	if _, err := PLMNKey(r.MCC, r.MNC); err != nil {
		return err
	}
	if strings.TrimSpace(r.Operator) == "" {
		return errors.New("运营商名称不能为空")
	}
	if err := validateTransport(r); err != nil {
		return err
	}
	if r.ResponseMode == ResponseSMS && !hasExpectedSender(r.ExpectedSenders) {
		return errors.New("短信回复规则必须至少设置一个预期发送者")
	}
	if strings.TrimSpace(r.ParserPattern) != "" {
		if _, err := regexp.Compile(r.ParserPattern); err != nil {
			return fmt.Errorf("余额解析正则无效: %w", err)
		}
	}
	return validateEvidenceURL(r.EvidenceURL)
}

func hasExpectedSender(senders []string) bool {
	for _, sender := range senders {
		if strings.TrimSpace(sender) != "" {
			return true
		}
	}
	return false
}

func validateTransport(r Rule) error {
	switch r.Transport {
	case TransportSMS:
		if strings.TrimSpace(r.Destination) == "" || strings.TrimSpace(r.Payload) == "" {
			return errors.New("SMS 规则必须设置目标号码和短信内容")
		}
		if r.ResponseMode != ResponseSMS {
			return errors.New("SMS 余额规则必须等待短信回复")
		}
	case TransportUSSD:
		if strings.TrimSpace(r.Payload) == "" {
			return errors.New("USSD 规则必须设置查询代码")
		}
		if r.ResponseMode != ResponseDirect && r.ResponseMode != ResponseSMS {
			return errors.New("USSD 回复模式必须是 direct 或 sms")
		}
	case TransportUnsupported:
		if strings.TrimSpace(r.Alternative) == "" {
			return errors.New("不支持的规则必须说明官方替代方式")
		}
		if r.ResponseMode != ResponseNone {
			return errors.New("不支持的规则回复模式必须是 none")
		}
	default:
		return fmt.Errorf("不支持的查询传输方式 %q", r.Transport)
	}
	return nil
}

func validateEvidenceURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("证据链接必须是有效的 HTTP(S) URL")
	}
	return nil
}

func PLMNKey(mcc, mnc string) (string, error) {
	mcc = strings.TrimSpace(mcc)
	mnc = strings.TrimSpace(mnc)
	if !digitsWithLength(mcc, 3, 3) || !digitsWithLength(mnc, 2, 3) {
		return "", errors.New("MCC 必须是 3 位数字，MNC 必须是 2 或 3 位数字")
	}
	numericMNC, err := strconv.Atoi(mnc)
	if err != nil {
		return "", errors.New("MNC 格式无效")
	}
	return fmt.Sprintf("%s:%d", mcc, numericMNC), nil
}

func digitsWithLength(value string, minLen, maxLen int) bool {
	if len(value) < minLen || len(value) > maxLen {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
