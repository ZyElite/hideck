package entitlement

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/internal/vowifi/entitlement/ts43"
	utls "github.com/refraction-networking/utls"
)

var (
	_ func(Request) (*Session, error) = NewSession
	_ HTTPClient                      = (*HTTPClientAdapter)(nil)
	_ ts43.HTTPClient                 = attHTTPClientAdapter{}
	_ http.RoundTripper               = fallbackRoundTripper{}
)

func TestLegacySessionRunsATTActionSequence(t *testing.T) {
	client := &recordingHTTPClient{response: &HTTPResponse{
		StatusCode: http.StatusOK,
		Body: []byte(`[
			{"status":1000,"response-id":3,"token":"next-token"},
			{"status":1000,"response-id":4,"response":[{"entitlement-name":"VoWiFi","entitlement-status":1}]},
			{"address-update-url":"https://address.example/e911","address-update-url-post-data":"token=abc"}
		]`),
	}}
	trace := &recordingTrace{}
	session, err := NewSession(Request{
		Provider: attEntitlementProvider, EntitlementURL: "https://entitlement.example/",
		Identity: Identity{
			IMSI: "310280233641503", ICCID: "89014103212345678901",
			IMEI: "356306952701762", MCC: "310", MNC: "280",
			SIPUsername: "310280233641503@private.att.net", DisplayName: "VoHive",
		},
		Token:  TokenState{Token: "cached", AppToken: "app"},
		Client: client, Trace: trace,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	url, data, contentType, title, err := session.StartE911AddressUpdate(context.Background())
	if err != nil {
		t.Fatalf("StartE911AddressUpdate: %v", err)
	}
	if url != "https://address.example/e911" || data != "token=abc" {
		t.Fatalf("websheet = %q %q", url, data)
	}
	if contentType != "application/x-www-form-urlencoded" || title != "E911地址" {
		t.Fatalf("metadata = %q %q", contentType, title)
	}
	assertATTRequest(t, client.request)
	if got := trace.events(); got != "request,response" {
		t.Fatalf("trace order = %q", got)
	}
}

func TestLegacySessionRejectsUnsupportedProviderAndMissingWebsheet(t *testing.T) {
	if _, err := NewSession(Request{Provider: "att"}); !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("unsupported provider error = %v", err)
	}
	session, err := NewSession(Request{
		Provider: attEntitlementProvider, EntitlementURL: "https://entitlement.example/",
		Client: &recordingHTTPClient{response: &HTTPResponse{
			StatusCode: http.StatusOK, Body: []byte(`[{"status":1000,"response-id":3}]`),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := session.StartE911AddressUpdate(nil); !errors.Is(err, ErrWebsheetUnavailable) {
		t.Fatalf("missing websheet error = %v", err)
	}
}

func TestATTAdapterPreservesAUTSSynchronizationFailure(t *testing.T) {
	auts := []byte("0123456789abcdef")
	adapter := attAKAProviderAdapter{provider: akaProviderFunc(func(_, _ []byte) (sim.AKAResult, error) {
		return sim.AKAResult{AUTS: auts}, sim.ErrSyncFailure
	})}
	result, err := adapter.CalculateAKAResult(make([]byte, 16), make([]byte, 16))
	if err != nil {
		t.Fatalf("CalculateAKAResult: %v", err)
	}
	if !bytes.Equal(result.AUTS, auts) {
		t.Fatalf("AUTS = %x", result.AUTS)
	}
	auts[0] = 0xff
	if result.AUTS[0] == auts[0] {
		t.Fatal("adapter retained the provider-owned AUTS buffer")
	}
}

func TestHTTPClientAdapterUsesRealHTTPAndCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		close(started)
		<-req.Context().Done()
	}))
	defer server.Close()

	adapter := NewHTTPClientAdapter(server.Client())
	contextual := adapter.(interface {
		DoContext(context.Context, *HTTPRequest) (*HTTPResponse, error)
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := contextual.DoContext(ctx, &HTTPRequest{Method: http.MethodPost, URL: server.URL})
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestFallbackRoundTripperReplaysBodyOverHTTP1(t *testing.T) {
	originalBody := &trackedBody{Reader: bytes.NewReader([]byte("payload"))}
	var primaryBody, fallbackBody string
	primaryResponseBody := &trackedBody{Reader: bytes.NewReader(nil)}
	transport := fallbackRoundTripper{
		primary: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			primaryBody = readBodyString(t, req.Body)
			return &http.Response{Body: primaryResponseBody}, errors.New(
				"http2: failed reading the frame payload: note that the frame header looked like an HTTP/1.1 header",
			)
		}),
		fallback: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			fallbackBody = readBodyString(t, req.Body)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		}),
	}
	req, err := http.NewRequest(http.MethodPost, "https://example.test/", originalBody)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	response.Body.Close()
	if primaryBody != "payload" || fallbackBody != "payload" {
		t.Fatalf("bodies primary=%q fallback=%q", primaryBody, fallbackBody)
	}
	if !originalBody.closed || !primaryResponseBody.closed {
		t.Fatalf("closed original=%v primary response=%v", originalBody.closed, primaryResponseBody.closed)
	}
}

func TestUTLSDialPerformsRealHandshakeAndALPN(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	address := strings.TrimPrefix(server.URL, "https://")
	connection, err := dialUTLS(context.Background(), "tcp", address, &utls.Config{
		ServerName: "example.test", InsecureSkipVerify: true, NextProtos: []string{"h2", "http/1.1"},
	})
	if err != nil {
		t.Fatalf("dialUTLS: %v", err)
	}
	defer connection.Close()
	state := connection.(*utls.UConn).ConnectionState()
	if state.NegotiatedProtocol != "h2" {
		t.Fatalf("ALPN = %q", state.NegotiatedProtocol)
	}
}

func assertATTRequest(t *testing.T, request *HTTPRequest) {
	t.Helper()
	if request == nil || request.Method != http.MethodPost || request.URL != "https://entitlement.example/" {
		t.Fatalf("request = %+v", request)
	}
	reader, err := gzip.NewReader(bytes.NewReader(request.Body))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	decompressed, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatalf("read gzip: %v", err)
	}
	var actions []map[string]interface{}
	if err := json.Unmarshal(decompressed, &actions); err != nil {
		t.Fatalf("decode actions: %v", err)
	}
	if len(actions) != 3 || actions[0]["action-name"] != "getAuthentication" ||
		actions[1]["action-name"] != "getEntitlement" ||
		actions[2]["action-name"] != "getPhoneServicesAccountStatus" {
		t.Fatalf("actions = %s", decompressed)
	}
}

type recordingHTTPClient struct {
	request  *HTTPRequest
	response *HTTPResponse
	err      error
}

func (c *recordingHTTPClient) Do(req *HTTPRequest) (*HTTPResponse, error) {
	c.request = req
	return c.response, c.err
}

type recordingTrace struct {
	mu    sync.Mutex
	items []string
}

func (t *recordingTrace) Request(*HTTPRequest)                 { t.append("request") }
func (t *recordingTrace) Response(*HTTPRequest, *HTTPResponse) { t.append("response") }
func (t *recordingTrace) Error(*HTTPRequest, error)            { t.append("error") }
func (t *recordingTrace) append(value string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.items = append(t.items, value)
}
func (t *recordingTrace) events() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Join(t.items, ",")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type trackedBody struct {
	*bytes.Reader
	closed bool
}

func (b *trackedBody) Close() error {
	b.closed = true
	return nil
}

func readBodyString(t *testing.T, reader io.ReadCloser) string {
	t.Helper()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(data)
}

type fixedAKAProvider struct{}

func (fixedAKAProvider) CalculateAKA(_, _ []byte) (sim.AKAResult, error) {
	return sim.AKAResult{}, nil
}

type akaProviderFunc func([]byte, []byte) (sim.AKAResult, error)

func (f akaProviderFunc) CalculateAKA(rand16, autn16 []byte) (sim.AKAResult, error) {
	return f(rand16, autn16)
}
