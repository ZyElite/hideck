package e911

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
	"github.com/iniwex5/vowifi-go/runtimehost/carrier"
)

type testAKAProvider struct {
	calls atomic.Int32
	rand  []byte
	autn  []byte
}

func (p *testAKAProvider) CalculateAKA(rand16, autn16 []byte) (enginesim.AKAResult, error) {
	p.calls.Add(1)
	p.rand = append([]byte(nil), rand16...)
	p.autn = append([]byte(nil), autn16...)
	return enginesim.AKAResult{
		RES: []byte{0x11, 0x22, 0x33, 0x44},
		CK:  make([]byte, 16),
		IK:  make([]byte, 16),
	}, nil
}

func TestStartEmergencyAddressUpdateUsesRealDefaultHTTPClient(t *testing.T) {
	var requestBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("x-protocol-version") != "2" {
			t.Errorf("request = %s protocol=%q", r.Method, r.Header.Get("x-protocol-version"))
		}
		requestBody, _ = io.ReadAll(r.Body)
		w.Header().Set("X-Entitlement", "accepted")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`[{"status":1000,"token":"abc123","title":"E911"}]`))
	}))
	defer server.Close()

	response, err := StartEmergencyAddressUpdate(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{E911: carrier.E911Config{
			Provider: "att-ts43", Websheet: "https://example.test/e911",
			EntitlementEndpoint: server.URL,
		}},
		Identity: Identity{IMSI: "310280233641503", IMEI: "356306952701762", MCC: "310", MNC: "280"},
	})
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdate: %v", err)
	}
	if !strings.Contains(response.URL, "token=abc123") || response.UserData != "abc123" || response.Title != "E911" {
		t.Fatalf("response = %+v", response)
	}
	if !strings.Contains(string(requestBody), `"operation":"emergency-address-update"`) || !strings.Contains(string(requestBody), `"imsi":"310280233641503"`) {
		t.Fatalf("request body = %s", requestBody)
	}
}

func TestStartEmergencyAddressUpdateAnswersAKAChallenge(t *testing.T) {
	rand16 := bytesFrom(0x10, 16)
	autn16 := bytesFrom(0x40, 16)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if call == 1 {
			_, _ = w.Write([]byte(`{"status":6004,"response-id":7,"rand":"` + hex.EncodeToString(rand16) + `","autn":"` + hex.EncodeToString(autn16) + `"}`))
			return
		}
		var payload []map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode challenge answer: %v", err)
		}
		if got := payload[0]["aka-res"]; got != "11223344" {
			t.Errorf("aka-res = %v", got)
		}
		_, _ = w.Write([]byte(`{"status":1000,"websheet-url":"https://example.test/address"}`))
	}))
	defer server.Close()
	aka := &testAKAProvider{}

	response, err := StartEmergencyAddressUpdate(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{E911: carrier.E911Config{
			Provider: "att-ts43", Websheet: "https://example.test/e911",
			EntitlementEndpoint: server.URL,
		}},
		Identity:    Identity{IMSI: "310280233641503", IMEI: "356306952701762", SIPUsername: "310280233641503@private.att.net"},
		AKAProvider: aka,
	})
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdate: %v", err)
	}
	if response.URL != "https://example.test/address" || requests.Load() != 2 || aka.calls.Load() != 1 {
		t.Fatalf("response=%+v requests=%d AKA calls=%d", response, requests.Load(), aka.calls.Load())
	}
	if string(aka.rand) != string(rand16) || string(aka.autn) != string(autn16) {
		t.Fatal("AKA provider did not receive the carrier challenge")
	}
}

func TestStartEmergencyAddressUpdateAnswersEAPRelayIdentity(t *testing.T) {
	identity := "310280233641503@private.att.net"
	requestPacket := eapaka.Packet{
		Code: eapaka.CodeRequest, Identifier: 6, Type: eapaka.TypeAKA,
		Subtype:    eapaka.SubtypeIdentity,
		Attributes: []eapaka.Attribute{eapaka.PermanentIDReqAttribute()},
	}
	raw, err := requestPacket.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	relay := base64.StdEncoding.EncodeToString(raw)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if requests.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"status":6004,"response-id":12,"eap-relay-packet":"` + relay + `"}`))
			return
		}
		assertEAPRelayIdentityAnswer(t, r.Body, identity)
		_, _ = w.Write([]byte(`{"status":1000,"websheet-url":"https://example.test/address"}`))
	}))
	defer server.Close()

	response, err := StartEmergencyAddressUpdate(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{E911: carrier.E911Config{
			Provider: "att-ts43", Websheet: "https://example.test/e911", EntitlementEndpoint: server.URL,
		}},
		Identity: Identity{IMSI: "310280233641503", IMEI: "356306952701762", SIPUsername: identity},
	})
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdate: %v", err)
	}
	if response.URL != "https://example.test/address" || requests.Load() != 2 {
		t.Fatalf("response=%+v requests=%d", response, requests.Load())
	}
}

func assertEAPRelayIdentityAnswer(t *testing.T, body io.Reader, wantIdentity string) {
	t.Helper()
	var payload []map[string]interface{}
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		t.Fatalf("decode identity answer: %v", err)
	}
	relay, _ := payload[0]["eap-relay-packet"].(string)
	raw, err := base64.StdEncoding.DecodeString(relay)
	if err != nil {
		t.Fatalf("decode relay: %v", err)
	}
	packet, err := eapaka.ParsePacket(raw)
	if err != nil {
		t.Fatalf("parse relay: %v", err)
	}
	attribute, ok := eapaka.FindAttribute(packet.Attributes, eapaka.AttributeIdentity)
	if !ok {
		t.Fatalf("identity response missing AT_IDENTITY: %+v", packet)
	}
	identity, err := attribute.IdentityValue()
	if err != nil || identity != wantIdentity {
		t.Fatalf("identity=%q err=%v", identity, err)
	}
}

func TestStartEmergencyAddressUpdatePropagatesHTTPFailureAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"upstream"}`))
	}))
	defer server.Close()
	request := Request{
		Carrier: carrier.EffectiveCarrierConfig{E911: carrier.E911Config{Provider: "att", EntitlementEndpoint: server.URL}},
	}
	if _, err := StartEmergencyAddressUpdate(context.Background(), request); err == nil || !strings.Contains(err.Error(), "HTTP status 502") {
		t.Fatalf("HTTP failure = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := StartEmergencyAddressUpdate(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request error = %v", err)
	}
}

func TestDefaultHTTPClientCopiesResponseMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Request") != "value" {
			t.Errorf("request header = %q", r.Header.Get("X-Request"))
		}
		w.Header().Add("X-Response", "one")
		w.Header().Add("X-Response", "two")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("accepted"))
	}))
	defer server.Close()

	resp, err := NewDefaultHTTPClient().Do(&HTTPRequest{
		Context: context.Background(), URL: server.URL, Method: http.MethodPut,
		Headers: []HeaderPair{{Key: "X-Request", Value: "value"}}, Body: []byte("payload"),
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted || string(resp.Body) != "accepted" || countHeader(resp.Headers, "X-Response") != 2 {
		t.Fatalf("response = %+v", resp)
	}
}

func bytesFrom(start byte, length int) []byte {
	out := make([]byte, length)
	for i := range out {
		out[i] = start + byte(i)
	}
	return out
}

func countHeader(headers []HeaderPair, key string) int {
	count := 0
	for _, header := range headers {
		if strings.EqualFold(header.Key, key) {
			count++
		}
	}
	return count
}
