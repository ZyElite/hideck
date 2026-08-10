package ussi

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
)

const (
	defaultLanguage = "en"
	defaultDCS      = 15
	emptyResponse   = "(空响应)"
)

// XMLPayload is the exact v1.5.5 USSI XML envelope.
type XMLPayload struct {
	XMLName    xml.Name `xml:"ussd-data"`
	Xmlns      string   `xml:"xmlns,attr"`
	Language   string   `xml:"language"`
	USSDString string   `xml:"ussd-string"`
}

// EncodeXML encodes one USSD string using the original media-type namespace.
func EncodeXML(text, language string) ([]byte, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("USSD command 为空")
	}
	language = strings.TrimSpace(language)
	if language == "" {
		language = defaultLanguage
	}
	payload := XMLPayload{Xmlns: ContentType, Language: language, USSDString: text}
	body, err := xml.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("编码 USSD XML 失败: %w", err)
	}
	return append([]byte(xml.Header), body...), nil
}

// DecodeXML decodes one USSI document.
func DecodeXML(body []byte) (*XMLPayload, error) {
	if len(body) == 0 {
		return nil, errors.New("USSD XML body 为空")
	}
	var payload XMLPayload
	if err := xml.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 USSD XML 失败: %w", err)
	}
	return &payload, nil
}

// IsContentType accepts both full and parameter-free USSI media types.
func IsContentType(contentType string) bool {
	value := strings.ToLower(strings.TrimSpace(contentType))
	return strings.Contains(value, ContentType) ||
		strings.Contains(value, "application/3gpp-ussd+xml")
}

// LooksLikeMenu requires at least two numbered choices, as in v1.5.5.
func LooksLikeMenu(message string) bool {
	choices := 0
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 2 || line[0] < '1' || line[0] > '9' {
			continue
		}
		switch line[1] {
		case '.', ')', ':', ' ':
			choices++
		}
	}
	return choices > 1
}

// ParseResult maps a SIP body into the v1.5.5 result contract.
func ParseResult(body []byte, sessionID string) *Result {
	result := &Result{DCS: defaultDCS}
	if len(body) == 0 {
		result.Text = emptyResponse
		return result
	}
	result.RawXML = string(body)
	xmlBody := ExtractFromMultipart(body)
	if len(xmlBody) == 0 {
		xmlBody = body
	}
	payload, err := DecodeXML(xmlBody)
	if err != nil {
		result.Text = string(body)
		return result
	}
	result.Text = payload.USSDString
	if LooksLikeMenu(result.Text) {
		result.Status = 1
		result.SessionID = sessionID
	}
	return result
}
