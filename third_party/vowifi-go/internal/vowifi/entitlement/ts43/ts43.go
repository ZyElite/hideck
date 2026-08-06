// Package ts43 implements the TS.43 (AT&T) VoWiFi entitlement check: the
// signed EAP-AKA challenge exchange with the entitlement server.
//
// Reconstructed from the decompiled internal/vowifi/entitlement/ts43.
package ts43

import (
	"bytes"
	"compress/gzip"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// EntitlementItem is one entitlement configuration item.
type EntitlementItem struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// EntitlementResponse is the parsed TS.43 response.
type EntitlementResponse struct {
	StatusCode int
	Items      []EntitlementItem
	Raw        map[string]interface{}
}

// BuildSubscriberID builds the subscriber ID (IMSI-based).
func BuildSubscriberID(imsi string) string {
	return "SUB_" + imsi
}

// BuildPermanentNAIIdentity builds the permanent NAI identity.
func BuildPermanentNAIIdentity(imsi, mcc, mnc string) string {
	if len(mnc) == 2 {
		mnc = "0" + mnc
	}
	return fmt.Sprintf("%s@nai.epc.mnc%s.mcc%s.3gppnetwork.org", imsi, mnc, mcc)
}

// BuildChallengePayload builds the EAP-AKA challenge payload for the
// entitlement request.
func BuildChallengePayload(imsi string, randBytes, autnBytes []byte) ([]byte, error) {
	payload := map[string]interface{}{
		"imsi":         imsi,
		"rand":         base64.StdEncoding.EncodeToString(randBytes),
		"autn":         base64.StdEncoding.EncodeToString(autnBytes),
		"nai_identity": imsi + "@nai.epc.mnc000.mcc000.3gppnetwork.org",
	}
	return json.Marshal(payload)
}

// deriveKAut derives the EAP-AKA authentication key from CK and IK.
func deriveKAut(ck, ik []byte) []byte {
	key := append(append([]byte{}, ck...), ik...)
	mac := hmac.New(sha1.New, key)
	mac.Write([]byte("EAP-AKA"))
	return mac.Sum(nil)[:16]
}

// buildSignedEAPResponse builds a signed EAP-AKA response.
func buildSignedEAPResponse(randBytes, autnBytes, res, ck, ik []byte) ([]byte, error) {
	kaut := deriveKAut(ck, ik)
	mac := hmac.New(sha1.New, kaut)
	mac.Write(randBytes)
	mac.Write(autnBytes)
	mac.Write(res)
	signature := mac.Sum(nil)[:16]

	payload := map[string]interface{}{
		"res":       base64.StdEncoding.EncodeToString(res),
		"signature": base64.StdEncoding.EncodeToString(signature),
	}
	return json.Marshal(payload)
}

// BuildAuthAction builds the auth action for the entitlement request.
func BuildAuthAction(imsi string, randBytes, autnBytes []byte) (map[string]interface{}, error) {
	challenge, err := BuildChallengePayload(imsi, randBytes, autnBytes)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"name":      "eap_aka",
		"challenge": string(challenge),
	}, nil
}

// DoJSONGzipRequest performs a JSON request and decodes the gzip JSON response.
func DoJSONGzipRequest(client *http.Client, url string, payload interface{}) (*EntitlementResponse, error) {
	if client == nil {
		client = http.DefaultClient
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := DecodeGzipBodyIfPresent(resp)
	if err != nil {
		return nil, err
	}
	return ParseResponse(resp.StatusCode, data)
}

// DecodeGzipBodyIfPresent decodes a gzip response body.
func DecodeGzipBodyIfPresent(resp *http.Response) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, errors.New("ts43: nil response")
	}
	if resp.Header.Get("Content-Encoding") == "gzip" {
		reader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return io.ReadAll(reader)
	}
	return io.ReadAll(resp.Body)
}

// ParseResponse parses a TS.43 response body.
func ParseResponse(statusCode int, data []byte) (*EntitlementResponse, error) {
	out := &EntitlementResponse{StatusCode: statusCode}
	if len(data) == 0 {
		return out, nil
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out.Raw = raw
	if items, ok := raw["entitlements"].([]interface{}); ok {
		for _, it := range items {
			if m, ok := it.(map[string]interface{}); ok {
				name, _ := m["name"].(string)
				enabled, _ := m["enabled"].(bool)
				out.Items = append(out.Items, EntitlementItem{Name: name, Enabled: enabled})
			}
		}
	}
	return out, nil
}

// IsVoWiFiEntitled reports whether the response entitles VoWiFi.
func (r *EntitlementResponse) IsVoWiFiEntitled() bool {
	if r == nil {
		return false
	}
	for _, item := range r.Items {
		if strings.EqualFold(item.Name, "vowifi") {
			return item.Enabled
		}
	}
	return false
}
