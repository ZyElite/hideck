package ussi

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
)

type Operation string

const (
	OperationRequest Operation = "request"
	OperationNotify  Operation = "notify"
)

// XMLPayload models the 3GPP USSI ussd-data document. Session and Dialog are
// retained for source compatibility with the first reconstruction.
type XMLPayload struct {
	XMLName         xml.Name  `xml:"ussd-data"`
	Language        string    `xml:"language,omitempty"`
	Text            string    `xml:"ussd-string"`
	Request         *struct{} `xml:"UnstructuredSS-Request,omitempty"`
	Notify          *struct{} `xml:"UnstructuredSS-Notify,omitempty"`
	ErrorCode       *int      `xml:"error-code,omitempty"`
	AlertingPattern *int      `xml:"alerting-pattern,omitempty"`
	Version         string    `xml:"-"`
	Session         struct {
		ID string `xml:"-"`
	} `xml:"-"`
	Dialog struct {
		Text string `xml:"-"`
	} `xml:"-"`
}

// EncodeXML encodes a standards-shaped USSI document.
func EncodeXML(payload *XMLPayload) ([]byte, error) {
	if payload == nil {
		return nil, errors.New("ussi: nil payload")
	}
	copy := *payload
	if copy.Text == "" {
		copy.Text = copy.Dialog.Text
	}
	if copy.Request == nil && copy.Notify == nil {
		copy.Request = &struct{}{}
	}
	body, err := xml.Marshal(&copy)
	if err != nil {
		return nil, fmt.Errorf("ussi: encode XML: %w", err)
	}
	return append([]byte(xml.Header), body...), nil
}

// DecodeXML decodes a USSI document and rejects unrelated XML roots.
func DecodeXML(body []byte) (*XMLPayload, error) {
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, errors.New("ussi: empty XML body")
	}
	var payload XMLPayload
	if err := xml.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("ussi: decode XML: %w", err)
	}
	if payload.XMLName.Local != "ussd-data" {
		return nil, fmt.Errorf("ussi: unexpected XML root %q", payload.XMLName.Local)
	}
	payload.Dialog.Text = payload.Text
	return &payload, nil
}

func requestXML(text string) ([]byte, error) {
	payload := &XMLPayload{Language: "en", Text: text, Request: &struct{}{}}
	return EncodeXML(payload)
}

// IsContentType reports whether a media type contains a USSI document.
func IsContentType(contentType string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	return mediaType == ContentType || mediaType == "application/vnd.3gpp.ussi+xml"
}

// LooksLikeMenu reports whether a network result requests another selection.
func LooksLikeMenu(message string) bool {
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if len(line) >= 2 && line[0] >= '1' && line[0] <= '9' && (line[1] == '.' || line[1] == ')') {
			return true
		}
	}
	return false
}

// ParseResult maps network text to the compatibility result shape.
func ParseResult(sessionID, body string) (*Result, error) {
	message := strings.TrimSpace(body)
	result := Result{SessionID: sessionID, Code: "0", Message: message, Done: true}
	if LooksLikeMenu(message) {
		result.Code = "1"
		result.Done = false
	}
	return &result, nil
}

func resultFromXML(sessionID string, body []byte) (Result, error) {
	payload, err := DecodeXML(body)
	if err != nil {
		return Result{}, err
	}
	done := payload.Notify != nil || payload.ErrorCode != nil
	result := Result{
		SessionID: sessionID,
		Code:      "1",
		Message:   strings.TrimSpace(payload.Text),
		RawXML:    string(body),
		Done:      done,
	}
	if done {
		result.Code = "0"
	}
	if payload.ErrorCode != nil {
		result.Code = fmt.Sprint(*payload.ErrorCode)
	}
	return result, nil
}
