package e911

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
	"github.com/iniwex5/vowifi-go/runtimehost/carrier"
)

const maxEntitlementChallengeResponses = 5

type entitlementResult struct {
	Status            int
	ResponseID        interface{}
	WebsheetURL       string
	UserData          string
	ContentType       string
	Title             string
	RAND              []byte
	AUTN              []byte
	ChallengeRequired bool
	EAPPacket         *eapaka.Packet
	EAPPacketRaw      []byte
}

func (r entitlementResult) hasChallenge() bool {
	return (len(r.RAND) == 16 && len(r.AUTN) == 16) ||
		(r.EAPPacket != nil && r.EAPPacket.Code == eapaka.CodeRequest)
}

func StartEmergencyAddressUpdate(ctx context.Context, req Request) (Response, error) {
	provider, websheetURL, endpoint := requestCarrierSettings(req)
	if !supportedProvider(provider) {
		return Response{}, ErrUnsupportedProvider
	}
	if endpoint == "" {
		if websheetURL == "" {
			return Response{}, ErrWebsheetUnavailable
		}
		return Response{URL: websheetURL, ContentType: "text/html", Title: "Emergency address"}, nil
	}
	return startEntitlementFlow(ctx, req, endpoint, websheetURL)
}

func requestCarrierSettings(req Request) (provider, websheetURL, endpoint string) {
	var cfg carrier.EffectiveCarrierConfig
	switch value := req.Carrier.(type) {
	case carrier.EffectiveCarrierConfig:
		cfg = value
	case *carrier.EffectiveCarrierConfig:
		if value != nil {
			cfg = *value
		}
	}
	provider = strings.ToLower(strings.TrimSpace(cfg.E911.Provider))
	websheetURL = strings.TrimSpace(cfg.E911.Websheet)
	endpoint = strings.TrimSpace(req.URL)
	if endpoint == "" {
		endpoint = strings.TrimSpace(cfg.E911.EntitlementEndpoint)
	}
	if provider == "" && endpoint != "" {
		provider = "att"
	}
	return provider, websheetURL, endpoint
}

func supportedProvider(provider string) bool {
	switch provider {
	case "att", "att-ts43", "ts43":
		return true
	default:
		return false
	}
}

func startEntitlementFlow(ctx context.Context, req Request, endpoint, fallbackURL string) (Response, error) {
	payload, err := initialEntitlementPayload(req.Identity)
	if err != nil {
		return Response{}, err
	}
	result, err := sendEntitlement(ctx, req, endpoint, payload)
	if err != nil {
		return Response{}, err
	}
	state := newEntitlementChallengeState(req)
	for attempts := 0; result.hasChallenge(); attempts++ {
		if attempts >= maxEntitlementChallengeResponses {
			return Response{}, fmt.Errorf("%w: too many entitlement challenges", ErrChallengeNotImplemented)
		}
		answer, err := challengeAnswer(req, result, &state)
		if err != nil {
			return Response{}, err
		}
		result, err = sendEntitlement(ctx, req, endpoint, answer)
		if err != nil {
			return Response{}, err
		}
	}
	if result.ChallengeRequired {
		return Response{}, ErrChallengeNotImplemented
	}
	response := websheetResponse(fallbackURL, result)
	if response.URL == "" {
		return Response{}, fmt.Errorf("%w: entitlement response did not include websheet data", ErrWebsheetUnavailable)
	}
	return state.applyToResponse(response), nil
}

func initialEntitlementPayload(identity Identity) ([]byte, error) {
	return json.Marshal([]map[string]interface{}{{
		"message-id":      1,
		"operation":       "emergency-address-update",
		"app-id":          "ap2003",
		"imsi":            identity.IMSI,
		"imei":            identity.IMEI,
		"mcc":             identity.MCC,
		"mnc":             identity.MNC,
		"sip-username":    identity.SIPUsername,
		"terminal-vendor": "vowifi-go",
	}})
}

func sendEntitlement(ctx context.Context, req Request, endpoint string, body []byte) (entitlementResult, error) {
	if err := ctx.Err(); err != nil {
		return entitlementResult{}, err
	}
	client := req.Client
	if client == nil {
		client = NewDefaultHTTPClient()
	}
	httpReq := &HTTPRequest{
		Context: ctx,
		Method:  "POST",
		URL:     endpoint,
		Headers: []HeaderPair{
			{Key: "Content-Type", Value: "application/json"},
			{Key: "Accept", Value: "application/json"},
			{Key: "x-protocol-version", Value: "2"},
		},
		Body: body,
	}
	trace, _ := req.Trace.(TraceSink)
	if trace != nil {
		trace.Request(httpReq)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		if trace != nil {
			trace.Error(httpReq, err)
		}
		return entitlementResult{}, err
	}
	if trace != nil {
		trace.Response(httpReq, resp)
	}
	if resp == nil {
		return entitlementResult{}, errors.New("e911 entitlement HTTP client returned nil response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return entitlementResult{}, fmt.Errorf("e911 entitlement HTTP status %d", resp.StatusCode)
	}
	return parseEntitlementResponse(resp.Body)
}

func parseEntitlementResponse(body []byte) (entitlementResult, error) {
	var root interface{}
	if err := json.Unmarshal(body, &root); err != nil {
		return entitlementResult{}, fmt.Errorf("e911: decode entitlement response: %w", err)
	}
	var result entitlementResult
	walkEntitlement(root, &result)
	return result, nil
}

func walkEntitlement(value interface{}, result *entitlementResult) {
	switch current := value.(type) {
	case []interface{}:
		for _, item := range current {
			walkEntitlement(item, result)
		}
	case map[string]interface{}:
		for key, item := range current {
			captureEntitlementField(strings.ToLower(strings.TrimSpace(key)), item, result)
			walkEntitlement(item, result)
		}
	}
}

func captureEntitlementField(key string, value interface{}, result *entitlementResult) {
	switch key {
	case "status":
		if status, ok := numberValue(value); ok {
			result.Status = status
			result.ChallengeRequired = status == 6004
		}
	case "response-id", "response_id", "responseid":
		result.ResponseID = value
	case "websheet", "websheet-url", "websheet_url", "e911-websheet-url", "e911_websheet_url", "address-url", "address_url", "url":
		if raw := stringValue(value); looksHTTPURL(raw) && result.WebsheetURL == "" {
			result.WebsheetURL = raw
		}
	case "user-data", "userdata", "user_data", "token", "entitlement-token", "entitlement_token", "auth-token", "auth_token":
		if raw := strings.TrimSpace(stringValue(value)); raw != "" && result.UserData == "" {
			result.UserData = raw
		}
	case "content-type", "content_type":
		result.ContentType = strings.TrimSpace(stringValue(value))
	case "title":
		result.Title = strings.TrimSpace(stringValue(value))
	case "rand":
		result.RAND, _ = decodeChallengeBytes(stringValue(value))
	case "autn":
		result.AUTN, _ = decodeChallengeBytes(stringValue(value))
	case "challenge", "aka-challenge", "aka_challenge":
		parseCombinedChallenge(value, result)
	case "eap-relay-packet", "eap_relay_packet", "eap-relay", "eap_relay":
		parseEAPRelayPacket(value, result)
	}
}

func websheetResponse(fallbackURL string, result entitlementResult) Response {
	websheetURL := strings.TrimSpace(result.WebsheetURL)
	userData := strings.TrimSpace(result.UserData)
	if websheetURL == "" && userData != "" {
		websheetURL = appendUserData(fallbackURL, userData)
	}
	return Response{
		URL:         websheetURL,
		UserData:    userData,
		ContentType: firstNonEmpty(result.ContentType, "text/html"),
		Title:       firstNonEmpty(result.Title, "Emergency address"),
	}
}

func appendUserData(rawURL, userData string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return ""
	}
	query := parsed.Query()
	if query.Get("token") == "" {
		query.Set("token", userData)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
