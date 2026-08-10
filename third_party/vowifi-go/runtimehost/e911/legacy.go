package e911

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/runtimehost/carrier"
	"github.com/iniwex5/vowifi-go/runtimehost/entitlement"
)

const attEntitlementProvider = "att_entitlement"

func NewDefaultHTTPClient() HTTPClient {
	return entitlementBackedHTTPClient{client: entitlement.NewDefaultHTTPClient()}
}

// StartEmergencyAddressUpdate is the v1.5.5 positional host API.
func StartEmergencyAddressUpdate(
	ctx context.Context,
	config carrier.EffectiveCarrierConfig,
	identity entitlement.Identity,
	token entitlement.TokenState,
	client HTTPClient,
	akaProvider sim.AKAProvider,
	trace TraceSink,
) (url, userData, contentType, title string, err error) {
	provider := strings.TrimSpace(config.E911.Provider)
	if client == nil {
		client = NewDefaultHTTPClient()
	}
	if provider != attEntitlementProvider {
		return "", "", "", "", fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
	return startATT(ctx, config, identity, token, client, akaProvider, trace)
}

func startATT(
	ctx context.Context,
	config carrier.EffectiveCarrierConfig,
	identity entitlement.Identity,
	token entitlement.TokenState,
	client HTTPClient,
	akaProvider sim.AKAProvider,
	trace TraceSink,
) (url, userData, contentType, title string, err error) {
	if config.DeviceIdentityEnabled && strings.TrimSpace(config.DeviceIdentityIMEI) != "" {
		identity.IMEI = strings.TrimSpace(config.DeviceIdentityIMEI)
	}
	session, err := entitlement.NewSession(entitlement.Request{
		Provider: config.E911.Provider, EntitlementURL: config.E911.EntitlementURL,
		Identity: identity, Token: token,
		Client:      entitlementHTTPClientAdapter{client: client, ctx: contextOrBackground(ctx)},
		AKAProvider: akaProvider,
		Trace:       entitlementTraceAdapter{trace: trace},
	})
	if err != nil {
		return "", "", "", "", mapEntitlementError(err)
	}
	url, userData, contentType, title, err = session.StartE911AddressUpdate(contextOrBackground(ctx))
	return url, userData, contentType, title, mapEntitlementError(err)
}

func startLegacyATTCurrent(ctx context.Context, req Request) (Response, error) {
	config, ok := carrierConfig(req.Carrier)
	if !ok {
		return Response{}, ErrUnsupportedProvider
	}
	if endpoint := strings.TrimSpace(req.URL); endpoint != "" {
		config.E911.EntitlementURL = endpoint
	} else if strings.TrimSpace(config.E911.EntitlementURL) == "" {
		config.E911.EntitlementURL = strings.TrimSpace(config.E911.EntitlementEndpoint)
	}
	url, data, contentType, title, err := StartEmergencyAddressUpdate(
		ctx, config, entitlementIdentity(req.Identity),
		entitlement.TokenState{Token: req.Identity.CachedToken},
		req.Client, akaProvider(req.AKAProvider), traceSink(req.Trace),
	)
	if err != nil {
		return Response{}, err
	}
	return Response{URL: url, UserData: data, ContentType: contentType, Title: title}, nil
}

func carrierConfig(value interface{}) (carrier.EffectiveCarrierConfig, bool) {
	switch config := value.(type) {
	case carrier.EffectiveCarrierConfig:
		return config, true
	case *carrier.EffectiveCarrierConfig:
		if config != nil {
			return *config, true
		}
	}
	return carrier.EffectiveCarrierConfig{}, false
}

func entitlementIdentity(value Identity) entitlement.Identity {
	return entitlement.Identity{
		IMSI: value.IMSI, ICCID: value.ICCID, IMEI: value.IMEI,
		MCC: value.MCC, MNC: value.MNC,
		SIPUsername: value.SIPUsername, DisplayName: value.DisplayName,
	}
}

func akaProvider(value interface{}) sim.AKAProvider {
	provider, _ := value.(sim.AKAProvider)
	return provider
}

func traceSink(value interface{}) TraceSink {
	trace, _ := value.(TraceSink)
	return trace
}

func mapEntitlementError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, entitlement.ErrUnsupportedProvider):
		return errors.Join(ErrUnsupportedProvider, err)
	case errors.Is(err, entitlement.ErrWebsheetUnavailable):
		return errors.Join(ErrWebsheetUnavailable, err)
	default:
		return err
	}
}

type entitlementHTTPClientAdapter struct {
	client HTTPClient
	ctx    context.Context
}

func (a entitlementHTTPClientAdapter) Do(req *entitlement.HTTPRequest) (*entitlement.HTTPResponse, error) {
	if a.client == nil {
		return nil, errors.New("e911 HTTP client is required")
	}
	response, err := a.client.Do(fromEntitlementHTTPRequest(req, a.ctx))
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("e911 HTTP client returned nil response")
	}
	return &entitlement.HTTPResponse{
		StatusCode: response.StatusCode, Body: cloneBytes(response.Body),
		Headers: toEntitlementHeaders(response.Headers),
	}, nil
}

type entitlementBackedHTTPClient struct {
	client entitlement.HTTPClient
}

func (a entitlementBackedHTTPClient) Do(req *HTTPRequest) (*HTTPResponse, error) {
	if a.client == nil {
		return nil, errors.New("e911 HTTP client is required")
	}
	request := toEntitlementHTTPRequest(req)
	ctx := context.Background()
	if req != nil {
		ctx = contextOrBackground(req.Context)
	}
	var response *entitlement.HTTPResponse
	var err error
	if contextual, ok := a.client.(interface {
		DoContext(context.Context, *entitlement.HTTPRequest) (*entitlement.HTTPResponse, error)
	}); ok {
		response, err = contextual.DoContext(ctx, request)
	} else {
		response, err = a.client.Do(request)
	}
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("e911 HTTP client returned nil response")
	}
	return &HTTPResponse{
		StatusCode: response.StatusCode, Body: cloneBytes(response.Body),
		Headers: fromEntitlementHeaders(response.Headers),
	}, nil
}

type entitlementTraceAdapter struct {
	trace TraceSink
}

func (a entitlementTraceAdapter) Request(req *entitlement.HTTPRequest) {
	if a.trace != nil {
		a.trace.Request(fromEntitlementHTTPRequest(req, nil))
	}
}

func (a entitlementTraceAdapter) Response(req *entitlement.HTTPRequest, resp *entitlement.HTTPResponse) {
	if a.trace != nil {
		a.trace.Response(fromEntitlementHTTPRequest(req, nil), fromEntitlementHTTPResponse(resp))
	}
}

func (a entitlementTraceAdapter) Error(req *entitlement.HTTPRequest, err error) {
	if a.trace != nil {
		a.trace.Error(fromEntitlementHTTPRequest(req, nil), err)
	}
}

func toEntitlementHTTPRequest(req *HTTPRequest) *entitlement.HTTPRequest {
	if req == nil {
		return nil
	}
	return &entitlement.HTTPRequest{
		Method: req.Method, URL: req.URL,
		Headers: toEntitlementHeaders(req.Headers), Body: cloneBytes(req.Body),
	}
}

func fromEntitlementHTTPRequest(req *entitlement.HTTPRequest, ctx context.Context) *HTTPRequest {
	if req == nil {
		return nil
	}
	return &HTTPRequest{
		Method: req.Method, URL: req.URL,
		Headers: fromEntitlementHeaders(req.Headers), Body: cloneBytes(req.Body), Context: ctx,
	}
}

func fromEntitlementHTTPResponse(resp *entitlement.HTTPResponse) *HTTPResponse {
	if resp == nil {
		return nil
	}
	return &HTTPResponse{
		StatusCode: resp.StatusCode, Body: cloneBytes(resp.Body),
		Headers: fromEntitlementHeaders(resp.Headers),
	}
}

func toEntitlementHeaders(headers []HeaderPair) []entitlement.HeaderPair {
	result := make([]entitlement.HeaderPair, len(headers))
	for index, header := range headers {
		result[index] = entitlement.HeaderPair{Key: header.Key, Value: header.Value}
	}
	return result
}

func fromEntitlementHeaders(headers []entitlement.HeaderPair) []HeaderPair {
	result := make([]HeaderPair, len(headers))
	for index, header := range headers {
		result[index] = HeaderPair{Key: header.Key, Value: header.Value}
	}
	return result
}

func cloneBytes(value []byte) []byte {
	return append([]byte(nil), value...)
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
