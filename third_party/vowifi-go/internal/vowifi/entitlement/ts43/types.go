package ts43

const (
	AuthTypeEAPAKA            = "EAP-AKA"
	ActionGetAuthentication   = "getAuthentication"
	ActionGetEntitlement      = "getEntitlement"
	ActionPostChallenge       = "postChallenge"
	EntitlementNameVoWiFi     = "VoWiFi"
	ProtocolVersion           = "2"
	EntitlementUserAgent      = "Entitlement/2.0 (iPhone) iOS/18.7.1"
	EntitlementAcceptLanguage = "zh-Hans-GB"
)

type AKAResult struct {
	RES  []byte
	CK   []byte
	IK   []byte
	AUTS []byte
}

type AKAProvider interface {
	CalculateAKAResult(rand16, autn16 []byte) (AKAResult, error)
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
}

type HTTPResponse struct {
	StatusCode int
	Body       []byte
}

type HTTPClient interface {
	Do(*HTTPRequest) (*HTTPResponse, error)
}

type AuthAction struct {
	AuthType     string `json:"auth-type"`
	ActionName   string `json:"action-name"`
	SubscriberID string `json:"subscriber-id"`
	RequestID    int    `json:"request-id"`
	UniqueID     string `json:"unique-id"`
	Token        string `json:"token,omitempty"`
}

type EntitlementAction struct {
	EntitlementNames []string `json:"entitlement-names"`
	ActionName       string   `json:"action-name"`
	RequestID        int      `json:"request-id"`
}

type PostChallengeAction struct {
	ActionName string `json:"action-name"`
	Payload    string `json:"payload"`
	RequestID  int    `json:"request-id"`
}

type EntitlementItem struct {
	EntitlementName   string `json:"entitlement-name"`
	EntitlementStatus int    `json:"entitlement-status"`
}

type responseItem struct {
	Status               int               `json:"status"`
	ResponseID           int               `json:"response-id"`
	Token                string            `json:"token"`
	AppToken             string            `json:"app-token"`
	Challenge            string            `json:"challenge"`
	ConnectivityAuthType string            `json:"connectivity-auth-type"`
	PhoneNumber          string            `json:"phone-number"`
	Signature            string            `json:"signature"`
	Response             []EntitlementItem `json:"response"`
}

// Response is the exported name for the response value returned by ParseResponse.
type Response = responseItem

// EntitlementResponse retains the name introduced by the source reconstruction.
type EntitlementResponse = responseItem

func (r responseItem) IsVoWiFiEntitled() bool {
	for _, item := range r.Response {
		if item.EntitlementName == EntitlementNameVoWiFi {
			return item.EntitlementStatus != 0
		}
	}
	return false
}
