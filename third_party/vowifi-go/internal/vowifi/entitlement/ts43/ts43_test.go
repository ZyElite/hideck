package ts43

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/iniwex5/vowifi-go/engine/eap"
)

const testIMSI = "310280233688494"

type testAKAProvider struct {
	result AKAResult
	rand   []byte
	autn   []byte
}

func (p *testAKAProvider) CalculateAKAResult(rand16, autn16 []byte) (AKAResult, error) {
	p.rand = append([]byte(nil), rand16...)
	p.autn = append([]byte(nil), autn16...)
	return p.result, nil
}

func TestBuildSubscriberIDMatchesV155Envelope(t *testing.T) {
	want := "AgAAOwEwMzEwMjgwMjMzNjg4NDk0QG5haS5lcGMubW5jMjgwLm1jYzMxMC4zZ3BwbmV0d29yay5vcmc="
	if got := BuildSubscriberID(testIMSI, "310", "280"); got != want {
		t.Fatalf("subscriber ID = %q", got)
	}
	if got := BuildPermanentNAIIdentity(testIMSI, "310", "280"); got != "0"+testIMSI+"@nai.epc.mnc280.mcc310.3gppnetwork.org" {
		t.Fatalf("permanent NAI = %q", got)
	}
	if got := BuildSubscriberID("", "310", "280"); got != "" {
		t.Fatalf("empty subscriber ID = %q", got)
	}
}

func TestRequestIDsStartAtRecoveredInitialValue(t *testing.T) {
	previous := requestSequence.Swap(initialRequestID)
	t.Cleanup(func() { requestSequence.Store(previous) })
	ids := NextRequestIDs(3)
	want := []int{3, 4, 5}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("request IDs = %v", ids)
		}
	}
}

func TestBuildAuthActionUsesRecoveredFields(t *testing.T) {
	action := BuildAuthAction(7, testIMSI, "iccid", " 356306952769025 ", "310", "280", "sip", "name", " token ", "app")
	if action.AuthType != "EAP-AKA" || action.ActionName != "getAuthentication" || action.RequestID != 7 {
		t.Fatalf("auth action = %+v", action)
	}
	if action.UniqueID != "356306952769025" || action.Token != "token" || action.SubscriberID == "" {
		t.Fatalf("auth identity = %+v", action)
	}
}

func TestBuildChallengePayloadSignsEAPAKAResponse(t *testing.T) {
	rand16 := sequence(0x10, 16)
	autn16 := sequence(0x40, 16)
	provider := &testAKAProvider{result: AKAResult{
		RES: []byte{0x11, 0x22, 0x33, 0x44}, CK: sequence(0x60, 16), IK: sequence(0x80, 16),
	}}
	payload, err := BuildChallengePayload(challengePacket(t, rand16, autn16), provider, testIMSI, "", "", "310", "280")
	if err != nil {
		t.Fatalf("BuildChallengePayload: %v", err)
	}
	if !bytes.Equal(provider.rand, rand16) || !bytes.Equal(provider.autn, autn16) {
		t.Fatalf("AKA input rand=%x autn=%x", provider.rand, provider.autn)
	}
	response := decodeEAPPacket(t, payload)
	if response.Code != eap.CodeResponse || response.Identifier != 9 || response.Subtype != eap.SubtypeChallenge {
		t.Fatalf("response packet = %+v", response)
	}
	attributes, err := eap.ParseAttributes(response.Data)
	if err != nil {
		t.Fatal(err)
	}
	if got := attributes[eap.AT_RES].Value; !bytes.Equal(got[2:6], provider.result.RES) {
		t.Fatalf("AT_RES = %x", got)
	}
	verifyResponseMAC(t, payload, deriveKAut(BuildPermanentNAIIdentity(testIMSI, "310", "280"), provider.result.IK, provider.result.CK))
}

func TestBuildChallengePayloadEncodesSynchronizationFailure(t *testing.T) {
	auts := sequence(0xa0, 14)
	provider := &testAKAProvider{result: AKAResult{AUTS: auts}}
	payload, err := BuildChallengePayload(challengePacket(t, sequence(1, 16), sequence(20, 16)), provider, testIMSI, "", "", "310", "280")
	if err != nil {
		t.Fatal(err)
	}
	response := decodeEAPPacket(t, payload)
	attributes, err := eap.ParseAttributes(response.Data)
	if err != nil {
		t.Fatal(err)
	}
	if response.Subtype != eap.SubtypeSyncFailure || !bytes.Equal(attributes[eap.AT_AUTS].Value[:14], auts) {
		t.Fatalf("sync response = %+v attrs=%x", response, response.Data)
	}
}

func TestBuildChallengePayloadRejectsMalformedChallenges(t *testing.T) {
	provider := &testAKAProvider{}
	missingRAND := (&eap.EAPPacket{
		Code: eap.CodeRequest, Identifier: 1, Type: eap.TypeAKA,
		Subtype: eap.SubtypeChallenge,
		Data:    fixedChallengeAttribute(eap.AT_AUTN, sequence(1, 16)),
	}).Encode()
	shortRAND := (&eap.EAPPacket{
		Code: eap.CodeRequest, Identifier: 1, Type: eap.TypeAKA,
		Subtype: eap.SubtypeChallenge,
		Data: append(
			fixedChallengeAttribute(eap.AT_RAND, sequence(1, 8)),
			fixedChallengeAttribute(eap.AT_AUTN, sequence(1, 16))...,
		),
	}).Encode()
	identity := (&eap.EAPPacket{Code: eap.CodeRequest, Identifier: 1, Type: eap.TypeIdentity}).Encode()
	tests := []struct {
		name      string
		challenge string
		want      string
	}{
		{name: "base64", challenge: "%%%", want: "decode challenge:"},
		{name: "packet", challenge: base64.StdEncoding.EncodeToString([]byte{1}), want: "parse challenge:"},
		{name: "type", challenge: base64.StdEncoding.EncodeToString(identity), want: "unsupported challenge type/subtype:"},
		{name: "missing RAND", challenge: base64.StdEncoding.EncodeToString(missingRAND), want: "challenge missing AT_RAND"},
		{name: "short RAND", challenge: base64.StdEncoding.EncodeToString(shortRAND), want: "AKA attribute shorter than 16 bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildChallengePayload(test.challenge, provider, testIMSI, "", "", "310", "280")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestParseResponseMergesControlPhoneAndEntitlements(t *testing.T) {
	body := []byte(`[
		{"status":1000,"response-id":3,"token":"token","challenge":"challenge"},
		{"status":6000,"response-id":5,"phone-number":"+15035550123","signature":"signed"},
		{"status":6000,"response-id":6,"response":[{"entitlement-name":"VoWiFi","entitlement-status":1}]}
	]`)
	response, err := ParseResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if response.Token != "token" || response.Challenge != "challenge" || response.PhoneNumber != "+15035550123" {
		t.Fatalf("merged response = %+v", response)
	}
	if !response.IsVoWiFiEntitled() || len(response.Response) != 1 {
		t.Fatalf("entitlements = %+v", response.Response)
	}
	if _, err := ParseResponse([]byte(`[]`)); err == nil || err.Error() != "empty TS43 entitlement response" {
		t.Fatalf("empty response error = %v", err)
	}
}

func TestDoJSONGzipRequestCompressesAndDecodes(t *testing.T) {
	client := &captureHTTPClient{responseBody: gzipBytes(t, []byte(`[{"status":1000,"response-id":3}]`))}
	response, err := DoJSONGzipRequest(context.Background(), client, "https://example.test/WFC", []interface{}{BuildEntitlementAction(4)}, []HeaderPair{{Key: "content-encoding", Value: "gzip"}})
	if err != nil {
		t.Fatal(err)
	}
	plain := gunzipBytes(t, client.request.Body)
	if !bytes.Contains(plain, []byte(`"action-name":"getEntitlement"`)) {
		t.Fatalf("request body = %s", plain)
	}
	if string(response.Body) != `[{"status":1000,"response-id":3}]` {
		t.Fatalf("response body = %s", response.Body)
	}
}

func TestDecodeGzipBodyIfPresentPreservesPlainAndTruncatedBodies(t *testing.T) {
	truncated := gzipBytes(t, []byte(`[{"status":1000}]`))
	truncated = truncated[:len(truncated)-4]
	for _, body := range [][]byte{[]byte(`[{"status":1000}]`), truncated} {
		decoded, err := DecodeGzipBodyIfPresent(body)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(decoded, body) {
			t.Fatalf("decoded = %x, want %x", decoded, body)
		}
	}
}

type captureHTTPClient struct {
	request      *HTTPRequest
	responseBody []byte
}

func (c *captureHTTPClient) Do(request *HTTPRequest) (*HTTPResponse, error) {
	c.request = request
	return &HTTPResponse{StatusCode: 200, Body: append([]byte(nil), c.responseBody...)}, nil
}

func challengePacket(t *testing.T, rand16, autn16 []byte) string {
	t.Helper()
	data := append(fixedChallengeAttribute(eap.AT_RAND, rand16), fixedChallengeAttribute(eap.AT_AUTN, autn16)...)
	raw := (&eap.EAPPacket{Code: eap.CodeRequest, Identifier: 9, Type: eap.TypeAKA, Subtype: eap.SubtypeChallenge, Data: data}).Encode()
	return base64.StdEncoding.EncodeToString(raw)
}

func fixedChallengeAttribute(attributeType uint8, value []byte) []byte {
	return (&eap.Attribute{Type: attributeType, Value: append([]byte{0, 0}, value...)}).Encode()
}

func decodeEAPPacket(t *testing.T, payload string) *eap.EAPPacket {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := eap.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func verifyResponseMAC(t *testing.T, payload string, key []byte) {
	t.Helper()
	raw, _ := base64.StdEncoding.DecodeString(payload)
	offset := bytes.Index(raw[8:], []byte{eap.AT_MAC, 5, 0, 0})
	if offset < 0 {
		t.Fatal("response missing AT_MAC")
	}
	offset += 12
	want := append([]byte(nil), raw[offset:offset+16]...)
	copy(raw[offset:offset+16], make([]byte, 16))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(raw)
	if !hmac.Equal(want, mac.Sum(nil)[:16]) {
		t.Fatalf("AT_MAC = %x", want)
	}
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := gzip.NewWriter(&out)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func gunzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	plain, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return plain
}

func sequence(start byte, length int) []byte {
	data := make([]byte, length)
	for i := range data {
		data[i] = start + byte(i)
	}
	return data
}

func TestActionJSONFieldNames(t *testing.T) {
	encoded, err := json.Marshal(BuildAuthAction(1, testIMSI, "", "imei", "310", "280", "", "", "", ""))
	if err != nil || !bytes.Contains(encoded, []byte(`"subscriber-id"`)) {
		t.Fatalf("auth JSON = %s err=%v", encoded, err)
	}
}
