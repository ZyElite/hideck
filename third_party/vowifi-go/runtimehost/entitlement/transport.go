package entitlement

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

const entitlementHTTPTimeout = 45 * time.Second

func NewDefaultHTTPClient() HTTPClient {
	return NewHTTPClientAdapter(&http.Client{
		Transport: &fallbackRoundTripper{
			primary:  newUTLSHTTP2Transport(),
			fallback: newUTLSHTTP1Transport(),
		},
		Timeout: entitlementHTTPTimeout,
	})
}

func NewHTTPClientAdapter(client *http.Client) HTTPClient {
	return &HTTPClientAdapter{client: client}
}

func (a *HTTPClientAdapter) Do(req *HTTPRequest) (*HTTPResponse, error) {
	return a.DoContext(context.Background(), req)
}

// DoContext is additive and lets current host adapters preserve cancellation
// while the v1.5.5 HTTPClient contract remains unchanged.
func (a *HTTPClientAdapter) DoContext(ctx context.Context, req *HTTPRequest) (*HTTPResponse, error) {
	if a == nil || a.client == nil {
		return nil, errors.New("entitlement HTTP client is required")
	}
	httpReq, err := newHTTPRequest(contextOrBackground(ctx), req)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("entitlement HTTP client returned nil response")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &HTTPResponse{
		StatusCode: resp.StatusCode, Body: body, Headers: flattenHTTPHeaders(resp.Header),
	}, nil
}

func flattenHTTPHeaders(headers http.Header) []HeaderPair {
	var result []HeaderPair
	for key, values := range headers {
		for _, value := range values {
			result = append(result, HeaderPair{Key: key, Value: value})
		}
	}
	return result
}

func newHTTPRequest(ctx context.Context, req *HTTPRequest) (*http.Request, error) {
	if req == nil {
		return nil, errors.New("entitlement HTTP request is nil")
	}
	if err := validateURL(req.URL); err != nil {
		return nil, err
	}
	method := strings.TrimSpace(req.Method)
	if method == "" {
		method = http.MethodGet
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		return nil, err
	}
	for _, header := range req.Headers {
		httpReq.Header.Add(header.Key, header.Value)
	}
	return httpReq, nil
}

func validateURL(raw string) error {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("entitlement invalid HTTP URL %q", raw)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("entitlement unsupported HTTP scheme %q", parsed.Scheme)
	}
	return nil
}

func newUTLSHTTP1Transport() http.RoundTripper {
	return &http.Transport{
		ForceAttemptHTTP2: false,
		DialTLSContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialUTLSWithNextProtos(ctx, network, address, []string{"http/1.1"})
		},
	}
}

func newUTLSHTTP2Transport() http.RoundTripper {
	return &http2.Transport{
		DialTLSContext: func(ctx context.Context, network, address string, _ *tls.Config) (net.Conn, error) {
			return dialUTLSWithNextProtos(ctx, network, address, []string{"h2", "http/1.1"})
		},
	}
}

func dialUTLSWithNextProtos(ctx context.Context, network, address string, nextProtos []string) (net.Conn, error) {
	ctx = contextOrBackground(ctx)
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	return dialUTLS(ctx, network, address, &utls.Config{
		ServerName: host, NextProtos: append([]string(nil), nextProtos...),
	})
}

func dialUTLS(ctx context.Context, network, address string, config *utls.Config) (net.Conn, error) {
	raw, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	conn := utls.UClient(raw, config, utls.HelloIOS_Auto)
	if err := conn.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, fmt.Errorf("uTLS handshake: %w", err)
	}
	return conn, nil
}

func (f fallbackRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if f.primary == nil || f.fallback == nil {
		return nil, errors.New("entitlement HTTP transports are required")
	}
	if req == nil {
		return nil, errors.New("entitlement HTTP request is nil")
	}
	body, hadBody, err := readRequestBody(req)
	if err != nil {
		return nil, err
	}
	if req.URL != nil && req.URL.Scheme == "http" {
		return f.fallback.RoundTrip(cloneRequestWithBody(req, body, hadBody))
	}
	resp, err := f.primary.RoundTrip(cloneRequestWithBody(req, body, hadBody))
	if err == nil || !isHTTP2SawHTTP1HeaderError(err) {
		return resp, err
	}
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	return f.fallback.RoundTrip(cloneRequestWithBody(req, body, hadBody))
}

func readRequestBody(req *http.Request) ([]byte, bool, error) {
	if req == nil || req.Body == nil {
		return nil, false, nil
	}
	body, readErr := io.ReadAll(req.Body)
	closeErr := req.Body.Close()
	if readErr != nil {
		return nil, true, readErr
	}
	return body, true, closeErr
}

func cloneRequestWithBody(req *http.Request, body []byte, hadBody bool) *http.Request {
	clone := req.Clone(contextOrBackground(req.Context()))
	if !hadBody {
		clone.Body = nil
		clone.GetBody = nil
		return clone
	}
	clone.Body = io.NopCloser(bytes.NewReader(body))
	clone.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	clone.ContentLength = int64(len(body))
	return clone
}

func isHTTP2SawHTTP1HeaderError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "http2: failed reading the frame payload") &&
		strings.Contains(message, "frame header looked like an HTTP/1.1 header")
}
