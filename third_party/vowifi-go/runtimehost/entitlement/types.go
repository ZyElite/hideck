// Package entitlement exposes the carrier entitlement host boundary.
package entitlement

import (
	"errors"
	"net/http"

	"github.com/iniwex5/vowifi-go/engine/sim"
)

var (
	ErrUnsupportedProvider = errors.New("entitlement provider unsupported")
	ErrWebsheetUnavailable = errors.New("entitlement websheet unavailable")
)

type HeaderPair struct {
	Key   string
	Value string
}

type HTTPRequest struct {
	Method  string
	URL     string
	Headers []HeaderPair
	Body    []byte
}

type HTTPResponse struct {
	StatusCode int
	Body       []byte

	// Headers is additive; v1.5.5 exposed status and body only.
	Headers []HeaderPair
}

type HTTPClient interface {
	Do(*HTTPRequest) (*HTTPResponse, error)
}

type TraceSink interface {
	Error(*HTTPRequest, error)
	Request(*HTTPRequest)
	Response(*HTTPRequest, *HTTPResponse)
}

type Identity struct {
	IMSI        string
	ICCID       string
	IMEI        string
	MCC         string
	MNC         string
	SIPUsername string
	DisplayName string
}

type TokenState struct {
	Token    string
	AppToken string
}

type Request struct {
	Provider       string
	EntitlementURL string
	Identity       Identity
	Token          TokenState
	Client         HTTPClient
	AKAProvider    sim.AKAProvider
	Trace          TraceSink
}

type Session struct {
	provider string
	att      *attSession
}

type attSession struct {
	req Request
}

type attHTTPClientAdapter struct {
	client HTTPClient
	trace  TraceSink
}

type attAKAProviderAdapter struct {
	provider sim.AKAProvider
}

type HTTPClientAdapter struct {
	client *http.Client
}

type fallbackRoundTripper struct {
	primary  http.RoundTripper
	fallback http.RoundTripper
}
