package policy

// E911Policy is the resolved emergency-address policy.
type E911Policy struct {
	Enabled             bool   `yaml:"enabled"`
	Provider            string `yaml:"provider"`
	EntitlementURL      string `yaml:"entitlement_url"`
	WebsheetHostPolicy  string `yaml:"websheet_host_policy"`
	Websheet            string
	EntitlementEndpoint string
}

// E911Config preserves the interim name used by current callers.
type E911Config = E911Policy

type IPSec3GPPSecurityMechanism struct {
	Alg  string `yaml:"alg"`
	EAlg string `yaml:"ealg"`
	Prot string `yaml:"prot"`
	Mode string `yaml:"mode"`
}

type IMSRegisterPolicy struct {
	ID                               string `yaml:"id"`
	TemporaryStatusCodes             []int  `yaml:"temporary_status_codes"`
	ForbiddenStatusCodes             []int  `yaml:"forbidden_status_codes"`
	InitialRejectFallbackStatusCodes []int  `yaml:"initial_reject_fallback_status_codes"`
	TemporaryRetrySeconds            int    `yaml:"temporary_retry_seconds"`
}

// IMSRegisterTemplate retains the recovered v1.5.5 fields first. The final
// fields preserve the interim policy surface until module 16 restores its
// resolver around the recovered model.
type IMSRegisterTemplate struct {
	ID                                  string
	UsePlainDigestPlaceholder           bool
	Expires                             int
	SMSReceiverTransport                string
	ContactMode                         string
	FixedPANI                           string
	SupportedHeader                     string
	AllowHeader                         string
	AccessType                          string
	ICSIRef                             string
	ContactParamOrder                   []string
	VoiceSupportedHeader                string
	VoiceAllowHeader                    string
	VoiceAcceptContact                  string
	VoicePPreferredService              string
	ForceHeaderPort5060                 bool
	IncludePANIAuthenticated            bool
	IncludeConnectionKeepaliveInAuth    bool
	SecAgreeMode                        string
	SecurityClientIncludesServerParams  bool
	SecurityClientMechanisms            []IPSec3GPPSecurityMechanism
	StrictSecurityServerOffer           bool
	EnableInitialRejectFallback         bool
	FallbackIncludesServerParamsInSecCl bool
	RegisterPolicy                      IMSRegisterPolicy

	Domain             string
	EPDGAddr           string
	Transport          string
	SMSRoutingMethod   string
	IdentitySource     string
	DNSServer          string
	ExpiresSeconds     int
	ContactOrder       []string
	RegisterPolicyMode string
	SecAgreeEnabled    bool
}

// EffectiveCarrierConfig is the recovered flattened carrier configuration.
// IMS is an additive compatibility projection for the interim policy API.
type EffectiveCarrierConfig struct {
	MCC                           string
	MNC                           string
	PresetID                      string
	MatchedTemplate               string
	E911                          E911Policy
	IPStackType                   string
	EPDGAddr                      string
	EPDGAddrSource                string
	EPDGPort                      uint16
	APN                           string
	DNSServer                     string
	NATKeepaliveSeconds           int
	DPDIntervalSeconds            int
	AKAChallengeMode              string
	IKEIdentityMode               string
	AKAIdentityMode               string
	IKEProposals                  []string
	ESPProposals                  []string
	EnableLegacyCiphers           bool
	AllowedLegacyCiphers          []string
	AlgorithmPolicy               string
	DeviceIdentityIMEI            string
	DeviceIdentityEnabled         bool
	DeviceModel                   string
	IMSDomain                     string
	IMSRealm                      string
	IMSRegistrar                  string
	IMSPCSCF                      string
	IMSUserAgent                  string
	IMSTransport                  string
	IMSIdentitySource             string
	IMSLocalPort                  int
	IMSTCPKeepaliveSeconds        int
	IMSOptionsPingIntervalSeconds int
	DPDKeepaliveIntervalSeconds   int
	ReauthIntervalSeconds         int
	IMSRegisterTemplate           IMSRegisterTemplate
	IMSRegisterPolicySource       string
	SMSRoutingMethod              string
	SMSRoutingGW                  string
	ForceSMSCAuth                 bool

	IMS IMSRegisterTemplate
}

// CarrierPlan preserves the interim plan shape until module 16 restores the
// recovered nested plan model.
type CarrierPlan struct {
	MCC      string
	MNC      string
	PresetID string
	E911     E911Policy
	IMS      IMSRegisterTemplate
}

// CarrierOverride preserves the interim override surface used by the current
// loader. The full recovered override model belongs to module 16.
type CarrierOverride struct {
	MCC      string
	MNC      string
	PresetID string
	E911     E911Policy
	IMS      IMSRegisterTemplate
}
