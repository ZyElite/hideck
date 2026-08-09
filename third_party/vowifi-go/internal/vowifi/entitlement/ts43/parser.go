package ts43

import (
	"encoding/json"
	"errors"
)

func ParseResponse(body []byte) (Response, error) {
	var items []responseItem
	if err := json.Unmarshal(body, &items); err != nil {
		return Response{}, err
	}
	if len(items) == 0 {
		return Response{}, errors.New("empty TS43 entitlement response")
	}
	var result Response
	for _, item := range items {
		if shouldUseControlResponse(item) {
			copyControlResponse(&result, item)
		}
		if item.PhoneNumber != "" || item.Signature != "" {
			result.PhoneNumber = item.PhoneNumber
			result.Signature = item.Signature
		}
		result.Response = append(result.Response, item.Response...)
	}
	return result, nil
}

func shouldUseControlResponse(item responseItem) bool {
	if item.Token != "" || item.AppToken != "" || item.Challenge != "" || item.ConnectivityAuthType != "" {
		return true
	}
	return (item.ResponseID == 3 || item.ResponseID == 4) &&
		len(item.Response) == 0 && item.PhoneNumber == "" && item.Signature == ""
}

func copyControlResponse(target *Response, source responseItem) {
	target.Status = source.Status
	target.ResponseID = source.ResponseID
	target.Token = source.Token
	target.AppToken = source.AppToken
	target.Challenge = source.Challenge
	target.ConnectivityAuthType = source.ConnectivityAuthType
}
