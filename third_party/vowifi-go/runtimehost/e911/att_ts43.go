package e911

import (
	"context"
	"errors"
	"fmt"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/internal/vowifi/entitlement/providers/att"
	"github.com/iniwex5/vowifi-go/internal/vowifi/entitlement/ts43"
)

func startATTS43Flow(ctx context.Context, req Request, endpoint, fallbackURL string) (Response, error) {
	result, err := att.CheckE911AddressUpdateRequest(ctx, att.E911UpdateRequest{
		URL:  endpoint,
		IMSI: req.Identity.IMSI, ICCID: req.Identity.ICCID, IMEI: req.Identity.IMEI,
		MCC: req.Identity.MCC, MNC: req.Identity.MNC,
		SIPUsername: req.Identity.SIPUsername, DisplayName: req.Identity.DisplayName,
		Token: req.Identity.CachedToken,
		Client: &attTS43HTTPClient{
			ctx: ctx, client: req.Client, trace: req.Trace,
		},
		AKAProvider: attTS43AKAProvider{provider: req.AKAProvider},
	})
	if err != nil {
		return Response{}, err
	}
	url, postData := result.Websheet()
	if url == "" {
		url = fallbackURL
	}
	if url == "" {
		return Response{}, fmt.Errorf("%w: entitlement response did not include address update URL", ErrWebsheetUnavailable)
	}
	return Response{
		URL: url, UserData: postData,
		ContentType: att.WebsheetContentType, Title: att.WebsheetTitle,
	}, nil
}

type attTS43HTTPClient struct {
	ctx    context.Context
	client HTTPClient
	trace  interface{}
}

func (c *attTS43HTTPClient) Do(request *ts43.HTTPRequest) (*ts43.HTTPResponse, error) {
	if request == nil {
		return nil, errors.New("e911: nil TS43 HTTP request")
	}
	ctx := c.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client := c.client
	if client == nil {
		client = NewDefaultHTTPClient()
	}
	httpRequest := &HTTPRequest{
		Context: ctx, Method: request.Method, URL: request.URL,
		Headers: toRuntimeHeaders(request.Headers),
		Body:    append([]byte(nil), request.Body...),
	}
	trace, _ := c.trace.(TraceSink)
	if trace != nil {
		trace.Request(httpRequest)
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		if trace != nil {
			trace.Error(httpRequest, err)
		}
		return nil, err
	}
	if response == nil {
		return nil, errors.New("e911: TS43 HTTP client returned nil response")
	}
	if trace != nil {
		trace.Response(httpRequest, response)
	}
	return &ts43.HTTPResponse{
		StatusCode: response.StatusCode,
		Body:       append([]byte(nil), response.Body...),
	}, nil
}

func toRuntimeHeaders(headers []ts43.HeaderPair) []HeaderPair {
	result := make([]HeaderPair, len(headers))
	for i, header := range headers {
		result[i] = HeaderPair{Key: header.Key, Value: header.Value}
	}
	return result
}

type attTS43AKAProvider struct {
	provider interface{}
}

func (a attTS43AKAProvider) CalculateAKAResult(rand16, autn16 []byte) (ts43.AKAResult, error) {
	provider, ok := a.provider.(enginesim.AKAProvider)
	if !ok || provider == nil {
		return ts43.AKAResult{}, ErrChallengeNotImplemented
	}
	result, err := provider.CalculateAKA(rand16, autn16)
	if errors.Is(err, enginesim.ErrSyncFailure) && len(result.AUTS) != 0 {
		err = nil
	}
	return ts43.AKAResult{
		RES: append([]byte(nil), result.RES...), CK: append([]byte(nil), result.CK...),
		IK: append([]byte(nil), result.IK...), AUTS: append([]byte(nil), result.AUTS...),
	}, err
}
