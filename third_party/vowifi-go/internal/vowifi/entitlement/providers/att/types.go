package att

import "github.com/iniwex5/vowifi-go/internal/vowifi/entitlement/ts43"

const (
	ActionGetPhoneServicesAccountStatus = "getPhoneServicesAccountStatus"
	WebsheetContentType                 = "application/x-www-form-urlencoded"
	WebsheetTitle                       = "E911地址"
)

type PhoneServicesAction struct {
	SIPUsername string `json:"sip-username"`
	ActionName  string `json:"action-name"`
	RequestID   int    `json:"request-id"`
	DisplayName string `json:"display-name"`
}

type responseItem struct {
	AddressUpdateURL      string `json:"address-update-url"`
	AddressUpdatePostData string `json:"address-update-url-post-data"`
	AddressRefID          string `json:"address-ref-id"`
	AddressRefIDExpiry    string `json:"address-ref-id-expiry"`
	TCStatus              int    `json:"tc-status"`
	AddressStatus         int    `json:"address-status"`
	ProvisioningStatus    int    `json:"provisioning-status"`
}

type Response struct {
	ts43.Response
	responseItem
	VoWiFiStatus int
}

func (r Response) Websheet() (url, postData string) {
	return r.AddressUpdateURL, r.AddressUpdatePostData
}

// E911UpdateRequest retains the additive request wrapper from the source
// reconstruction while exposing the complete old identity and token inputs.
type E911UpdateRequest struct {
	URL         string
	IMSI        string
	ICCID       string
	IMEI        string
	MCC         string
	MNC         string
	SIPUsername string
	DisplayName string
	Token       string
	AppToken    string
	Client      ts43.HTTPClient
	AKAProvider ts43.AKAProvider
}
