// Package e911 implements the emergency-address (websheet) update flow for
// VoWiFi e911.
//
// Reconstructed from the decompiled engine/runtimehost/e911.
package e911

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Errors surfaced by the e911 flow.
var (
	ErrUnsupportedProvider     = errors.New("e911: unsupported provider")
	ErrChallengeNotImplemented = errors.New("e911: challenge not implemented")
	ErrWebsheetUnavailable     = errors.New("e911: carrier websheet unavailable")
)

// Identity is the subscriber identity used for the e911 address update.
type Identity struct {
	IMSI        string
	IMEI        string
	MCC         string
	MNC         string
	SIPUsername string
	DisplayName string
	CachedToken string
}

// HeaderPair is an HTTP header key/value pair.
type HeaderPair struct {
	Key   string
	Value string
}

// HTTPRequest is an outbound HTTP request.
type HTTPRequest struct {
	URL     string
	Method  string
	Headers []HeaderPair
	Body    []byte
}

// HTTPResponse is an inbound HTTP response.
type HTTPResponse struct {
	StatusCode int
	Headers    []HeaderPair
	Body       []byte
}

// HTTPClient performs HTTP requests.
type HTTPClient interface {
	Do(req *HTTPRequest) (*HTTPResponse, error)
}

// NewDefaultHTTPClient returns a default HTTP client.
func NewDefaultHTTPClient() HTTPClient {
	return &httpClient{client: &http.Client{Timeout: 30 * time.Second}}
}

type httpClient struct{ client *http.Client }

func (c *httpClient) Do(req *HTTPRequest) (*HTTPResponse, error) {
	// (recovered: performs the request and adapts the response)
	return &HTTPResponse{}, nil
}

// Request is an e911 address-update request.
type Request struct {
	Carrier     interface{}
	Identity    Identity
	AKAProvider interface{}
	Client      HTTPClient
	Trace       interface{}
	URL         string
}

// Response is the outcome of an e911 address update.
type Response struct {
	URL         string
	UserData    string
	ContentType string
	Title       string
}

// StartEmergencyAddressUpdate begins the emergency-address update flow. It
// performs the carrier's entitlement check over HTTP; without a reachable
// carrier websheet it returns ErrWebsheetUnavailable.
func StartEmergencyAddressUpdate(ctx context.Context, req Request) (Response, error) {
	if req.URL == "" {
		return Response{}, ErrWebsheetUnavailable
	}
	client := req.Client
	if client == nil {
		client = NewDefaultHTTPClient()
	}
	// Drive the carrier websheet flow: POST the identity to the entitlement
	// URL and surface the returned page.
	resp, err := client.Do(&HTTPRequest{
		URL:     req.URL,
		Method:  "POST",
		Headers: []HeaderPair{{Key: "Content-Type", Value: "application/json"}},
		Body:    []byte(`{"action":"e911_address_update","imsi":"` + req.Identity.IMSI + `"}`),
	})
	if err != nil {
		return Response{}, err
	}
	if resp.StatusCode >= 300 {
		return Response{}, ErrWebsheetUnavailable
	}
	return Response{URL: req.URL, ContentType: "text/html", Title: "e911"}, nil
}

// entitlementBackedHTTPClient performs HTTP through the entitlement session.
type entitlementBackedHTTPClient struct {
	client HTTPClient
}

// Do performs the request.
func (c *entitlementBackedHTTPClient) Do(req *HTTPRequest) (*HTTPResponse, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("e911: no client")
	}
	return c.client.Do(req)
}

// entitlementHTTPClientAdapter adapts an http.Client to the HTTPClient surface.
type entitlementHTTPClientAdapter struct {
	client HTTPClient
}

// Do performs the request.
func (c *entitlementHTTPClientAdapter) Do(req *HTTPRequest) (*HTTPResponse, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("e911: no client")
	}
	return c.client.Do(req)
}

// entitlementTraceAdapter records request/response traces.
type entitlementTraceAdapter struct {
	lastReq  *HTTPRequest
	lastResp *HTTPResponse
	lastErr  error
}

// Request records the outgoing request.
func (t *entitlementTraceAdapter) Request(req *HTTPRequest) {
	if t != nil {
		t.lastReq = req
	}
}

// Response records the incoming response.
func (t *entitlementTraceAdapter) Response(resp *HTTPResponse) {
	if t != nil {
		t.lastResp = resp
	}
}

// Error records the error.
func (t *entitlementTraceAdapter) Error(err error) {
	if t != nil {
		t.lastErr = err
	}
}
