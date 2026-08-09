package att

import (
	"context"
	"errors"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/entitlement/ts43"
)

var entitlementHeaders = []ts43.HeaderPair{
	{Key: "accept", Value: "application/json"},
	{Key: "content-type", Value: "application/json"},
	{Key: "accept-encoding", Value: "gzip"},
	{Key: "x-protocol-version", Value: ts43.ProtocolVersion},
	{Key: "content-encoding", Value: "gzip"},
	{Key: "user-agent", Value: ts43.EntitlementUserAgent},
	{Key: "accept-language", Value: ts43.EntitlementAcceptLanguage},
}

// CheckE911AddressUpdate preserves the original v1.5.5 positional API.
func CheckE911AddressUpdate(
	ctx context.Context,
	url string,
	imsi, iccid, imei, mcc, mnc string,
	sipUsername, displayName string,
	token, appToken string,
	client ts43.HTTPClient,
	akaProvider ts43.AKAProvider,
) (Response, error) {
	if client == nil {
		return Response{}, errors.New("e911 HTTP client is required")
	}
	input := actionInput{
		IMSI: imsi, ICCID: iccid, IMEI: imei, MCC: mcc, MNC: mnc,
		SIPUsername: sipUsername, DisplayName: displayName,
		Token: token, AppToken: appToken,
	}
	result, err := runInitialActions(ctx, url, input, client)
	if err != nil || result.Challenge == "" || akaProvider == nil {
		return result, err
	}
	return answerChallenge(ctx, url, input, result, client, akaProvider)
}

func CheckE911AddressUpdateRequest(ctx context.Context, request E911UpdateRequest) (Response, error) {
	return CheckE911AddressUpdate(
		ctx, request.URL,
		request.IMSI, request.ICCID, request.IMEI, request.MCC, request.MNC,
		request.SIPUsername, request.DisplayName,
		request.Token, request.AppToken,
		request.Client, request.AKAProvider,
	)
}

type actionInput struct {
	IMSI        string
	ICCID       string
	IMEI        string
	MCC         string
	MNC         string
	SIPUsername string
	DisplayName string
	Token       string
	AppToken    string
}

func runInitialActions(ctx context.Context, url string, input actionInput, client ts43.HTTPClient) (Response, error) {
	count := 2
	if strings.TrimSpace(input.DisplayName) != "" {
		count = 3
	}
	return runActions(ctx, url, buildInitialActions(ts43.NextRequestIDs(count), input), client)
}

func answerChallenge(
	ctx context.Context,
	url string,
	input actionInput,
	initial Response,
	client ts43.HTTPClient,
	akaProvider ts43.AKAProvider,
) (Response, error) {
	payload, err := ts43.BuildChallengePayload(
		initial.Challenge, akaProvider,
		input.IMSI, input.ICCID, input.IMEI, input.MCC, input.MNC,
	)
	if err != nil {
		return initial, err
	}
	post, err := runActions(ctx, url, []interface{}{ts43.PostChallengeAction{
		ActionName: ts43.ActionPostChallenge,
		Payload:    payload,
		RequestID:  ts43.NextRequestIDs(1)[0],
	}}, client)
	if err != nil {
		return post, err
	}
	if post.AddressUpdateURL != "" || post.AddressUpdatePostData != "" {
		return post, nil
	}
	input.Token = firstNonEmpty(post.Token, input.Token)
	input.AppToken = firstNonEmpty(post.AppToken, input.AppToken)
	return runInitialActions(ctx, url, input, client)
}

func runActions(ctx context.Context, url string, actions []interface{}, client ts43.HTTPClient) (Response, error) {
	response, err := ts43.DoJSONGzipRequest(ctx, client, strings.TrimSpace(url), actions, entitlementHeaders)
	if err != nil {
		return Response{}, err
	}
	return ParseResponse(response.Body)
}

func buildInitialActions(requestIDs []int, input actionInput) []interface{} {
	actions := []interface{}{
		ts43.BuildAuthAction(
			requestIDs[0], input.IMSI, input.ICCID, input.IMEI, input.MCC, input.MNC,
			input.SIPUsername, input.DisplayName, input.Token, input.AppToken,
		),
		ts43.BuildEntitlementAction(requestIDs[1]),
	}
	if strings.TrimSpace(input.DisplayName) == "" {
		return actions
	}
	return append(actions, PhoneServicesAction{
		SIPUsername: strings.TrimSpace(input.SIPUsername),
		ActionName:  ActionGetPhoneServicesAccountStatus,
		RequestID:   requestIDs[2],
		DisplayName: strings.TrimSpace(input.DisplayName),
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
