// Package entitlement implements the TS.43 entitlement session used to
// check VoWiFi entitlement and drive the E911 address update.
//
// Reconstructed from the decompiled engine/runtimehost/entitlement.
package entitlement

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
)

// Session is a TS.43 entitlement session.
type Session struct {
	client *http.Client
	url    string
	att    *attSession
}

// NewSession creates an entitlement session.
func NewSession(client *http.Client, url string) *Session {
	if client == nil {
		client = http.DefaultClient
	}
	return &Session{client: client, url: url}
}

// StartE911AddressUpdate begins the E911 address update flow.
func (s *Session) StartE911AddressUpdate(ctx context.Context) error {
	if s == nil {
		return errors.New("entitlement: nil session")
	}
	if s.att == nil {
		s.att = newATTSession(s.client, s.url)
	}
	return s.att.StartE911AddressUpdate(ctx)
}

// attSession is the AT&T-specific entitlement session.
type attSession struct {
	client *http.Client
	url    string
}

// newATTSession creates an AT&T session.
func newATTSession(client *http.Client, url string) *attSession {
	if client == nil {
		client = http.DefaultClient
	}
	return &attSession{client: client, url: url}
}

// StartE911AddressUpdate drives the AT&T E911 address update.
func (s *attSession) StartE911AddressUpdate(ctx context.Context) error {
	if s == nil {
		return errors.New("entitlement: nil att session")
	}
	if s.url == "" {
		return errors.New("entitlement: no entitlement URL")
	}
	req, err := http.NewRequestWithContext(ctx, "POST", s.url, strings.NewReader(`{"action":"e911_address_update"}`))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return errors.New("entitlement: e911 update failed with status " + http.StatusText(resp.StatusCode))
	}
	return nil
}

// HTTPClientAdapter adapts an http.Client to the entitlement HTTP surface.
type HTTPClientAdapter struct {
	client *http.Client
}

// Do performs an HTTP request.
func (a *HTTPClientAdapter) Do(req *http.Request) (*http.Response, error) {
	if a == nil || a.client == nil {
		return nil, errors.New("entitlement: nil http client")
	}
	return a.client.Do(req)
}

// attHTTPClientAdapter is the AT&T HTTP client adapter.
type attHTTPClientAdapter struct {
	client *http.Client
}

// Do performs an HTTP request.
func (a *attHTTPClientAdapter) Do(req *http.Request) (*http.Response, error) {
	if a == nil || a.client == nil {
		return nil, errors.New("entitlement: nil http client")
	}
	return a.client.Do(req)
}

// attAKAProviderAdapter adapts an AKA provider for the AT&T flow.
type attAKAProviderAdapter struct {
	provider interface {
		CalculateAKA(rand16, autn16 []byte) (res, ck, ik []byte, err error)
	}
}

// CalculateAKAResult computes the AKA result.
func (a *attAKAProviderAdapter) CalculateAKAResult(rand16, autn16 []byte) (res, ck, ik []byte, err error) {
	if a == nil || a.provider == nil {
		return nil, nil, nil, errors.New("entitlement: no AKA provider")
	}
	return a.provider.CalculateAKA(rand16, autn16)
}

// fallbackRoundTripper falls back from HTTP/2 to HTTP/1.1.
type fallbackRoundTripper struct {
	base http.RoundTripper
}

// RoundTrip performs the request, falling back to HTTP/1.1 on HTTP/2 errors.
func (f *fallbackRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if f == nil || f.base == nil {
		return nil, errors.New("entitlement: nil round tripper")
	}
	resp, err := f.base.RoundTrip(req)
	if err != nil && isHTTP2SawHTTP1HeaderError(err) {
		// Retry over HTTP/1.1.
		req2 := cloneRequestWithBody(req)
		req2.Proto = "HTTP/1.1"
		req2.ProtoMajor = 1
		req2.ProtoMinor = 1
		return f.base.RoundTrip(req2)
	}
	return resp, err
}

// cloneRequestWithBody clones an HTTP request, preserving the body.
func cloneRequestWithBody(req *http.Request) *http.Request {
	if req == nil {
		return nil
	}
	clone := req.Clone(req.Context())
	if req.Body != nil {
		body, _ := readRequestBody(req.Body)
		clone.Body = io.NopCloser(bytes.NewReader(body))
	}
	return clone
}

// readRequestBody reads a request body fully.
func readRequestBody(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	return io.ReadAll(r)
}

// dialUTLSWithNextProtos dials a TLS connection with ALPN next-protos.
func dialUTLSWithNextProtos(ctx context.Context, network, addr string, nextProtos []string) (interface{}, error) {
	// The entitlement client uses the standard library TLS; this hook is
	// retained for the recovered uTLS path.
	return nil, errors.New("entitlement: uTLS dial not available")
}

// isHTTP2SawHTTP1HeaderError reports whether err is the HTTP/2 "saw HTTP/1
// header" error.
func isHTTP2SawHTTP1HeaderError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "saw HTTP/1 header")
}
