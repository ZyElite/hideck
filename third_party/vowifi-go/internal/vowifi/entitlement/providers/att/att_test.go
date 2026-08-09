package att

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/iniwex5/vowifi-go/engine/eap"
	"github.com/iniwex5/vowifi-go/internal/vowifi/entitlement/ts43"
)

type scriptedHTTPClient struct {
	mu        sync.Mutex
	requests  []*ts43.HTTPRequest
	responses [][]byte
}

func (c *scriptedHTTPClient) Do(request *ts43.HTTPRequest) (*ts43.HTTPResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, request)
	body := c.responses[len(c.requests)-1]
	return &ts43.HTTPResponse{StatusCode: 200, Body: body}, nil
}

type fixedAKAProvider struct{}

func (fixedAKAProvider) CalculateAKAResult(_, _ []byte) (ts43.AKAResult, error) {
	return ts43.AKAResult{
		RES: []byte{1, 2, 3, 4}, CK: bytes.Repeat([]byte{5}, 16), IK: bytes.Repeat([]byte{6}, 16),
	}, nil
}

func TestCheckE911AddressUpdateRunsRecoveredActionSequence(t *testing.T) {
	client := &scriptedHTTPClient{responses: [][]byte{
		[]byte(`[{"status":1000,"response-id":3,"challenge":"` + testChallenge(t) + `"}]`),
		[]byte(`[{"status":1000,"response-id":3,"token":"new-token","app-token":"new-app"}]`),
		[]byte(`[{"status":6000,"response-id":5,"response":[{"entitlement-name":"VoWiFi","entitlement-status":1}],"address-update-url":"https://example.test/address","address-update-url-post-data":"auth=token","address-ref-id":"ref","tc-status":1}]`),
	}}
	response, err := CheckE911AddressUpdate(
		context.Background(), "https://example.test/WFC",
		"310280233688494", "8901", "356306952769025", "310", "280",
		"310280233688494@private.att.net", "VoHive", "old-token", "old-app",
		client, fixedAKAProvider{},
	)
	if err != nil {
		t.Fatalf("CheckE911AddressUpdate: %v", err)
	}
	if response.AddressUpdateURL != "https://example.test/address" || response.AddressUpdatePostData != "auth=token" || response.VoWiFiStatus != 1 {
		t.Fatalf("response = %+v", response)
	}
	if len(client.requests) != 3 {
		t.Fatalf("requests = %d", len(client.requests))
	}
	initial := decodeActions(t, client.requests[0].Body)
	post := decodeActions(t, client.requests[1].Body)
	repeated := decodeActions(t, client.requests[2].Body)
	assertActionNames(t, initial, "getAuthentication", "getEntitlement", "getPhoneServicesAccountStatus")
	assertActionNames(t, post, "postChallenge")
	assertActionNames(t, repeated, "getAuthentication", "getEntitlement", "getPhoneServicesAccountStatus")
	if repeated[0]["token"] != "new-token" {
		t.Fatalf("repeated auth token = %v", repeated[0]["token"])
	}
	assertRecoveredHeaders(t, client.requests[0].Headers)
}

func TestCheckE911AddressUpdateRequiresHTTPClient(t *testing.T) {
	_, err := CheckE911AddressUpdate(
		context.Background(), "https://example.test/WFC",
		"imsi", "", "imei", "310", "280", "", "", "", "", nil, nil,
	)
	if err == nil || err.Error() != "e911 HTTP client is required" {
		t.Fatalf("error = %v", err)
	}
}

func TestCheckE911AddressUpdatePropagatesHTTPError(t *testing.T) {
	want := errors.New("entitlement transport failed")
	_, err := CheckE911AddressUpdate(
		context.Background(), "https://example.test/WFC",
		"imsi", "", "imei", "310", "280", "", "", "", "", failingHTTPClient{err: want}, nil,
	)
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
}

func TestNextRequestIDsAreUniqueUnderConcurrency(t *testing.T) {
	const workers = 64
	results := make(chan int, workers)
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			results <- ts43.NextRequestIDs(1)[0]
		}()
	}
	group.Wait()
	close(results)
	seen := make(map[int]struct{}, workers)
	for requestID := range results {
		if _, duplicate := seen[requestID]; duplicate {
			t.Fatalf("duplicate request ID %d", requestID)
		}
		seen[requestID] = struct{}{}
	}
	if len(seen) != workers {
		t.Fatalf("request IDs = %d, want %d", len(seen), workers)
	}
}

type failingHTTPClient struct {
	err error
}

func (c failingHTTPClient) Do(*ts43.HTTPRequest) (*ts43.HTTPResponse, error) {
	return nil, c.err
}

func testChallenge(t *testing.T) string {
	t.Helper()
	rand16 := bytes.Repeat([]byte{0x11}, 16)
	autn16 := bytes.Repeat([]byte{0x22}, 16)
	data := append(fixedAttribute(eap.AT_RAND, rand16), fixedAttribute(eap.AT_AUTN, autn16)...)
	raw := (&eap.EAPPacket{
		Code: eap.CodeRequest, Identifier: 4, Type: eap.TypeAKA,
		Subtype: eap.SubtypeChallenge, Data: data,
	}).Encode()
	return base64.StdEncoding.EncodeToString(raw)
}

func fixedAttribute(attributeType uint8, value []byte) []byte {
	return (&eap.Attribute{Type: attributeType, Value: append([]byte{0, 0}, value...)}).Encode()
}

func decodeActions(t *testing.T, compressed []byte) []map[string]interface{} {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var actions []map[string]interface{}
	if err := json.Unmarshal(body, &actions); err != nil {
		t.Fatalf("decode actions %s: %v", body, err)
	}
	return actions
}

func assertActionNames(t *testing.T, actions []map[string]interface{}, names ...string) {
	t.Helper()
	if len(actions) != len(names) {
		t.Fatalf("actions = %+v", actions)
	}
	for i, name := range names {
		if actions[i]["action-name"] != name {
			t.Fatalf("action %d = %+v", i, actions[i])
		}
	}
}

func assertRecoveredHeaders(t *testing.T, headers []ts43.HeaderPair) {
	t.Helper()
	values := make(map[string]string, len(headers))
	for _, header := range headers {
		values[header.Key] = header.Value
	}
	for key, want := range map[string]string{
		"accept":             "application/json",
		"content-type":       "application/json",
		"accept-encoding":    "gzip",
		"content-encoding":   "gzip",
		"x-protocol-version": "2",
		"user-agent":         "Entitlement/2.0 (iPhone) iOS/18.7.1",
		"accept-language":    "zh-Hans-GB",
	} {
		if values[key] != want {
			t.Fatalf("header %s = %q", key, values[key])
		}
	}
}
