package att

import (
	"encoding/json"
	"errors"

	"github.com/iniwex5/vowifi-go/internal/vowifi/entitlement/ts43"
)

func ParseResponse(body []byte) (Response, error) {
	common, err := ts43.ParseResponse(body)
	if err != nil {
		return Response{}, err
	}
	var providerItems []responseItem
	if err := json.Unmarshal(body, &providerItems); err != nil {
		return Response{}, err
	}
	if len(providerItems) == 0 {
		return Response{}, errors.New("empty AT&T entitlement response")
	}
	result := Response{Response: common}
	for _, item := range common.Response {
		if item.EntitlementName == ts43.EntitlementNameVoWiFi {
			result.VoWiFiStatus = item.EntitlementStatus
			break
		}
	}
	for _, item := range providerItems {
		if item.AddressUpdateURL != "" || item.AddressUpdatePostData != "" {
			result.responseItem = item
		}
	}
	return result, nil
}
