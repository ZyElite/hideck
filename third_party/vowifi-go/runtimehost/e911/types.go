// Package e911 implements the emergency-address entitlement and websheet flow.
package e911

import (
	"context"
	"errors"
	"io"

	"github.com/iniwex5/vowifi-go/engine/swu"
)

var (
	ErrUnsupportedProvider     = errors.New("e911 provider unsupported")
	ErrChallengeNotImplemented = errors.New("e911 challenge not implemented")
	ErrWebsheetUnavailable     = errors.New("e911 websheet unavailable")
)

type Identity struct {
	IMSI        string
	ICCID       string
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
	Method  string
	URL     string
	Headers []HeaderPair
	Body    []byte

	// Context is an additive field used by the current cancellation-aware API.
	Context context.Context
}

type HTTPResponse struct {
	StatusCode int
	Body       []byte

	// Headers is additive; the v1.5.5 boundary exposed status and body only.
	Headers []HeaderPair
}

type HTTPClient interface {
	Do(req *HTTPRequest) (*HTTPResponse, error)
}

type TraceSink interface {
	Request(*HTTPRequest)
	Response(*HTTPRequest, *HTTPResponse)
	Error(*HTTPRequest, error)
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

type WebsheetRequest = Response
