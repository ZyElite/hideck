package entitlement

import (
	"context"
	"errors"
	"fmt"
	"strings"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/internal/vowifi/entitlement/providers/att"
	"github.com/iniwex5/vowifi-go/internal/vowifi/entitlement/ts43"
)

const attEntitlementProvider = "att_entitlement"

func NewSession(req Request) (*Session, error) {
	provider := strings.TrimSpace(req.Provider)
	if provider != attEntitlementProvider {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
	return &Session{provider: provider, att: newATTSession(req)}, nil
}

func newATTSession(req Request) *attSession {
	if req.Client == nil {
		req.Client = NewDefaultHTTPClient()
	}
	return &attSession{req: req}
}

func (s *Session) StartE911AddressUpdate(ctx context.Context) (
	url, userData, contentType, title string,
	err error,
) {
	if s == nil || s.att == nil {
		return "", "", "", "", fmt.Errorf("%w: session is unavailable", ErrUnsupportedProvider)
	}
	return s.att.StartE911AddressUpdate(ctx)
}

func (s *attSession) StartE911AddressUpdate(ctx context.Context) (
	url, userData, contentType, title string,
	err error,
) {
	if s == nil {
		return "", "", "", "", errors.New("entitlement ATT session is nil")
	}
	req := s.req
	result, err := att.CheckE911AddressUpdate(
		contextOrBackground(ctx), strings.TrimSpace(req.EntitlementURL),
		req.Identity.IMSI, req.Identity.ICCID, selectedIMEI(req),
		req.Identity.MCC, req.Identity.MNC,
		req.Identity.SIPUsername, req.Identity.DisplayName,
		req.Token.Token, req.Token.AppToken,
		attHTTPClientAdapter{client: req.Client, trace: req.Trace},
		attAKAProviderAdapter{provider: req.AKAProvider},
	)
	if err != nil {
		return "", "", "", "", err
	}
	url, userData = result.Websheet()
	if strings.TrimSpace(url) == "" || strings.TrimSpace(userData) == "" {
		return "", "", "", "", ErrWebsheetUnavailable
	}
	return url, userData, att.WebsheetContentType, att.WebsheetTitle, nil
}

func selectedIMEI(req Request) string {
	return strings.TrimSpace(req.Identity.IMEI)
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (a attHTTPClientAdapter) Do(req *ts43.HTTPRequest) (*ts43.HTTPResponse, error) {
	if a.client == nil {
		return nil, errors.New("entitlement HTTP client is required")
	}
	hostReq := fromTS43HTTPRequest(req)
	if a.trace != nil {
		a.trace.Request(hostReq)
	}
	resp, err := a.client.Do(hostReq)
	if err != nil {
		if a.trace != nil {
			a.trace.Error(hostReq, err)
		}
		return nil, err
	}
	if resp == nil {
		err = errors.New("entitlement HTTP client returned nil response")
		if a.trace != nil {
			a.trace.Error(hostReq, err)
		}
		return nil, err
	}
	if a.trace != nil {
		a.trace.Response(hostReq, resp)
	}
	return &ts43.HTTPResponse{StatusCode: resp.StatusCode, Body: cloneBytes(resp.Body)}, nil
}

func (a attAKAProviderAdapter) CalculateAKAResult(rand16, autn16 []byte) (ts43.AKAResult, error) {
	if a.provider == nil {
		return ts43.AKAResult{}, errors.New("entitlement AKA provider is required")
	}
	result, err := a.provider.CalculateAKA(cloneBytes(rand16), cloneBytes(autn16))
	if errors.Is(err, enginesim.ErrSyncFailure) && len(result.AUTS) != 0 {
		err = nil
	}
	return ts43.AKAResult{
		RES: cloneBytes(result.RES), CK: cloneBytes(result.CK),
		IK: cloneBytes(result.IK), AUTS: cloneBytes(result.AUTS),
	}, err
}

func fromTS43HTTPRequest(req *ts43.HTTPRequest) *HTTPRequest {
	if req == nil {
		return nil
	}
	headers := make([]HeaderPair, len(req.Headers))
	for index, header := range req.Headers {
		headers[index] = HeaderPair{Key: header.Key, Value: header.Value}
	}
	return &HTTPRequest{
		Method: req.Method, URL: req.URL, Headers: headers, Body: cloneBytes(req.Body),
	}
}

func cloneBytes(value []byte) []byte {
	return append([]byte(nil), value...)
}
