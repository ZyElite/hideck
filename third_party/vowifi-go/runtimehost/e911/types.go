// Package e911 implements the emergency-address entitlement and websheet flow.
package e911

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/engine/swu"
)

const defaultHTTPTimeout = 30 * time.Second

var (
	ErrUnsupportedProvider     = errors.New("e911: unsupported provider")
	ErrChallengeNotImplemented = errors.New("e911: challenge not implemented")
	ErrWebsheetUnavailable     = errors.New("e911: carrier websheet unavailable")
)

type Identity struct {
	IMSI        string
	IMEI        string
	MCC         string
	MNC         string
	SIPUsername string
	DisplayName string
	CachedToken string
}

type HeaderPair struct {
	Key   string
	Value string
}

type HTTPRequest struct {
	Context context.Context
	URL     string
	Method  string
	Headers []HeaderPair
	Body    []byte
}

type HTTPResponse struct {
	StatusCode int
	Headers    []HeaderPair
	Body       []byte
}

type HTTPClient interface {
	Do(req *HTTPRequest) (*HTTPResponse, error)
}

type TraceSink interface {
	Request(*HTTPRequest)
	Response(*HTTPRequest, *HTTPResponse)
	Error(*HTTPRequest, error)
}

func NewDefaultHTTPClient() HTTPClient {
	return &httpClient{client: &http.Client{Timeout: defaultHTTPTimeout}}
}

type httpClient struct {
	client *http.Client
}

func (c *httpClient) Do(req *HTTPRequest) (*HTTPResponse, error) {
	if req == nil {
		return nil, errors.New("e911: nil HTTP request")
	}
	if err := validateHTTPURL(req.URL); err != nil {
		return nil, err
	}
	ctx := req.Context
	if ctx == nil {
		ctx = context.Background()
	}
	method := strings.TrimSpace(req.Method)
	if method == "" {
		method = http.MethodGet
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		return nil, fmt.Errorf("e911: build HTTP request: %w", err)
	}
	for _, header := range req.Headers {
		httpReq.Header.Add(header.Key, header.Value)
	}
	client := c.client
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("e911: HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("e911: read HTTP response: %w", err)
	}
	return &HTTPResponse{
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
		Body:       body,
	}, nil
}

func validateHTTPURL(rawURL string) error {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("e911: invalid HTTP URL %q", rawURL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("e911: unsupported HTTP URL scheme %q", parsed.Scheme)
	}
	return nil
}

func flattenHeaders(headers http.Header) []HeaderPair {
	var pairs []HeaderPair
	for key, values := range headers {
		for _, value := range values {
			pairs = append(pairs, HeaderPair{Key: key, Value: value})
		}
	}
	return pairs
}

type Request struct {
	Carrier             interface{}
	Identity            Identity
	AKAProvider         interface{}
	EAPReauthentication swu.EAPReauthenticationState
	Client              HTTPClient
	Trace               interface{}
	Random              io.Reader
	URL                 string
}

type Response struct {
	URL                 string
	UserData            string
	ContentType         string
	Title               string
	EAPNextPseudonym    string
	EAPNextReauthID     string
	EAPReauthentication swu.EAPReauthenticationState
}
