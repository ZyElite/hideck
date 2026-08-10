package e911

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
	"github.com/iniwex5/vowifi-go/runtimehost/carrier"
	"github.com/iniwex5/vowifi-go/runtimehost/entitlement"
)

var _ func(
	context.Context,
	carrier.EffectiveCarrierConfig,
	entitlement.Identity,
	entitlement.TokenState,
	HTTPClient,
	sim.AKAProvider,
	TraceSink,
) (string, string, string, string, error) = StartEmergencyAddressUpdate

func TestLegacyStartEmergencyAddressUpdateRunsRecoveredATTFlow(t *testing.T) {
	client := &legacyCaptureClient{response: legacyWebsheetResponse()}
	config := carrier.EffectiveCarrierConfig{
		E911: carrier.E911Policy{
			Provider: attEntitlementProvider, EntitlementURL: "https://entitlement.example/",
		},
		DeviceIdentityEnabled: true, DeviceIdentityIMEI: "override-imei",
	}
	url, data, contentType, title, err := StartEmergencyAddressUpdate(
		context.Background(), config,
		entitlement.Identity{
			IMSI: "310280233641503", ICCID: "89014103212345678901",
			IMEI: "identity-imei", MCC: "310", MNC: "280", DisplayName: "VoHive",
		},
		entitlement.TokenState{}, client, nil, nil,
	)
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdate: %v", err)
	}
	if url != "https://address.example/e911" || data != "token=abc" {
		t.Fatalf("websheet = %q %q", url, data)
	}
	if contentType != "application/x-www-form-urlencoded" || title != "E911地址" {
		t.Fatalf("metadata = %q %q", contentType, title)
	}
	if got := authActionField(t, client.request.Body, "unique-id"); got != "override-imei" {
		t.Fatalf("unique-id = %v", got)
	}
}

func TestCurrentATTEntitlementPresetUsesLegacyProductionChain(t *testing.T) {
	var requestBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requestBody = gunzipBody(t, req.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(legacyWebsheetResponse().Body)
	}))
	defer server.Close()

	response, err := StartEmergencyAddressUpdateCurrent(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{E911: carrier.E911Policy{
			Provider: attEntitlementProvider, EntitlementURL: server.URL,
		}},
		Identity: Identity{
			IMSI: "310280233641503", ICCID: "89014103212345678901",
			IMEI: "356306952701762", MCC: "310", MNC: "280", DisplayName: "VoHive",
		},
	})
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdateCurrent: %v", err)
	}
	if response.URL != "https://address.example/e911" || response.UserData != "token=abc" {
		t.Fatalf("response = %+v", response)
	}
	if !bytes.Contains(requestBody, []byte(`"action-name":"getAuthentication"`)) {
		t.Fatalf("request body = %s", requestBody)
	}
}

func TestLegacyStartMapsProviderAndWebsheetErrors(t *testing.T) {
	_, _, _, _, err := StartEmergencyAddressUpdate(
		context.Background(), carrier.EffectiveCarrierConfig{},
		entitlement.Identity{}, entitlement.TokenState{}, nil, nil, nil,
	)
	if !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("provider error = %v", err)
	}

	config := carrier.EffectiveCarrierConfig{E911: carrier.E911Policy{
		Provider: attEntitlementProvider, EntitlementURL: "https://entitlement.example/",
	}}
	client := &legacyCaptureClient{response: &HTTPResponse{
		StatusCode: http.StatusOK, Body: []byte(`[{"status":1000,"response-id":3}]`),
	}}
	_, _, _, _, err = StartEmergencyAddressUpdate(
		context.Background(), config, entitlement.Identity{}, entitlement.TokenState{}, client, nil, nil,
	)
	if !errors.Is(err, ErrWebsheetUnavailable) || !errors.Is(err, entitlement.ErrWebsheetUnavailable) {
		t.Fatalf("websheet error = %v", err)
	}
}

func TestDefaultHTTPClientRejectsNilRequestWithoutPanic(t *testing.T) {
	if response, err := NewDefaultHTTPClient().Do(nil); err == nil || response != nil {
		t.Fatalf("nil request response=%+v error=%v", response, err)
	}
}

func TestCurrentFlowCompletesEAPFastReauthentication(t *testing.T) {
	identity := "reauth-id@wlan.mnc280.mcc310.3gppnetwork.org"
	keys, err := eapaka.DeriveKeys(identity, sim.AKAResult{
		RES: []byte{1, 2, 3, 4}, CK: bytes.Repeat([]byte{0x11}, 16), IK: bytes.Repeat([]byte{0x22}, 16),
	})
	if err != nil {
		t.Fatalf("DeriveKeys: %v", err)
	}
	nonceS := []byte("0123456789abcdef")
	relay := signedReauthenticationRelay(t, keys, 9, nonceS)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			_, _ = w.Write([]byte(`{"status":6004,"response-id":9,"eap-relay-packet":"` + relay + `"}`))
			return
		}
		assertReauthenticationAnswer(t, req.Body, keys, nonceS)
		_, _ = w.Write([]byte(`{"status":1000,"websheet-url":"https://address.example/e911"}`))
	}))
	defer server.Close()

	response, err := StartEmergencyAddressUpdateCurrent(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{E911: carrier.E911Policy{
			Provider: "att", EntitlementEndpoint: server.URL,
		}},
		EAPReauthentication: swu.EAPReauthenticationState{
			Identity: identity, Keys: keys, Counter: 8, CounterOK: true,
		},
		Random: bytes.NewReader(bytes.Repeat([]byte{0x44}, 32)),
	})
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdateCurrent: %v", err)
	}
	if requests != 2 || response.URL != "https://address.example/e911" {
		t.Fatalf("requests=%d response=%+v", requests, response)
	}
	state := response.EAPReauthentication
	if !state.Reauthenticated || !state.CounterOK || state.Counter != 9 {
		t.Fatalf("reauth state = %+v", state)
	}
}

func signedReauthenticationRelay(t *testing.T, keys eapaka.Keys, counter uint16, nonceS []byte) string {
	t.Helper()
	iv := bytes.Repeat([]byte{0x55}, 16)
	encrypted, err := eapaka.EncryptAttributes(keys.KEncr, iv, []eapaka.Attribute{
		eapaka.CounterAttribute(counter), eapaka.NonceSAttribute(nonceS),
	})
	if err != nil {
		t.Fatalf("EncryptAttributes: %v", err)
	}
	packet := eapaka.Packet{
		Code: eapaka.CodeRequest, Identifier: 15, Type: eapaka.TypeAKA,
		Subtype: eapaka.SubtypeReauthentication,
		Attributes: []eapaka.Attribute{
			eapaka.IVAttribute(iv), encrypted, eapaka.MACAttribute(nil),
		},
	}
	raw, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	mac, err := eapaka.CalculateMAC(keys.KAut, raw, nil)
	if err != nil {
		t.Fatalf("CalculateMAC: %v", err)
	}
	packet.Attributes[len(packet.Attributes)-1] = eapaka.MACAttribute(mac)
	raw, err = packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary signed: %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func assertReauthenticationAnswer(t *testing.T, body io.Reader, keys eapaka.Keys, nonceS []byte) {
	t.Helper()
	var payload []map[string]interface{}
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		t.Fatalf("decode reauth answer: %v", err)
	}
	encoded, _ := payload[0]["eap-relay-packet"].(string)
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode relay: %v", err)
	}
	packet, err := eapaka.ParsePacket(raw)
	if err != nil {
		t.Fatalf("parse relay: %v", err)
	}
	if packet.Code != eapaka.CodeResponse || packet.Subtype != eapaka.SubtypeReauthentication {
		t.Fatalf("response packet = %+v", packet)
	}
	if err := eapaka.VerifyMAC(keys.KAut, raw, nonceS); err != nil {
		t.Fatalf("VerifyMAC: %v", err)
	}
}

func legacyWebsheetResponse() *HTTPResponse {
	return &HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`[
		{"status":1000,"response-id":3},
		{"status":1000,"response-id":4,"response":[{"entitlement-name":"VoWiFi","entitlement-status":1}]},
		{"address-update-url":"https://address.example/e911","address-update-url-post-data":"token=abc"}
	]`)}
}

func authActionField(t *testing.T, compressed []byte, field string) interface{} {
	t.Helper()
	var actions []map[string]interface{}
	if err := json.Unmarshal(gunzipBytes(t, compressed), &actions); err != nil {
		t.Fatalf("decode actions: %v", err)
	}
	if len(actions) == 0 {
		t.Fatal("missing entitlement actions")
	}
	return actions[0][field]
}

func gunzipBody(t *testing.T, body io.Reader) []byte {
	t.Helper()
	compressed, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return gunzipBytes(t, compressed)
}

func gunzipBytes(t *testing.T, compressed []byte) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer reader.Close()
	plain, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read gzip: %v", err)
	}
	return plain
}

type legacyCaptureClient struct {
	request  *HTTPRequest
	response *HTTPResponse
	err      error
}

func (c *legacyCaptureClient) Do(req *HTTPRequest) (*HTTPResponse, error) {
	c.request = req
	return c.response, c.err
}
