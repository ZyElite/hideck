package policy

type E911Policy struct {
	Enabled            bool   `yaml:"enabled"`
	Provider           string `yaml:"provider"`
	EntitlementURL     string `yaml:"entitlement_url"`
	WebsheetHostPolicy string `yaml:"websheet_host_policy"`

	// Compatibility fields added after v1.5.5.
	Websheet            string
	EntitlementEndpoint string
}

type E911Config = E911Policy

type E911PolicyOverride struct {
	Enabled            *bool  `yaml:"enabled"`
	Provider           string `yaml:"provider"`
	EntitlementURL     string `yaml:"entitlement_url"`
	WebsheetHostPolicy string `yaml:"websheet_host_policy"`

	// Compatibility fields added after v1.5.5.
	Websheet            string `yaml:"websheet"`
	EntitlementEndpoint string `yaml:"entitlement_endpoint"`
}

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

type IMSRegisterPolicyOverride struct {
	ID                               string `yaml:"id"`
	TemporaryStatusCodes             *[]int `yaml:"temporary_status_codes"`
	ForbiddenStatusCodes             *[]int `yaml:"forbidden_status_codes"`
	InitialRejectFallbackStatusCodes *[]int `yaml:"initial_reject_fallback_status_codes"`
	TemporaryRetrySeconds            *int   `yaml:"temporary_retry_seconds"`
}

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

	// Compatibility fields added after v1.5.5.
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

type IMSRegisterTemplateOverride struct {
	ID                                  string                       `yaml:"id"`
	UsePlainDigestPlaceholder           *bool                        `yaml:"use_plain_digest_placeholder"`
	Expires                             *int                         `yaml:"expires"`
	SMSReceiverTransport                string                       `yaml:"sms_receiver_transport"`
	ContactMode                         string                       `yaml:"contact_mode"`
	FixedPANI                           string                       `yaml:"fixed_pani"`
	SupportedHeader                     string                       `yaml:"supported_header"`
	AllowHeader                         string                       `yaml:"allow_header"`
	AccessType                          string                       `yaml:"access_type"`
	ICSIRef                             string                       `yaml:"icsi_ref"`
	ContactParamOrder                   []string                     `yaml:"contact_param_order"`
	ForceHeaderPort5060                 *bool                        `yaml:"force_header_port_5060"`
	IncludePANIAuthenticated            *bool                        `yaml:"include_pani_authenticated"`
	IncludeConnectionKeepaliveInAuth    *bool                        `yaml:"include_connection_keepalive_in_auth"`
	SecAgreeMode                        string                       `yaml:"sec_agree_mode"`
	SecurityClientIncludesServerParams  *bool                        `yaml:"security_client_includes_server_params"`
	SecurityClientMechanisms            []IPSec3GPPSecurityMechanism `yaml:"security_client_mechanisms"`
	StrictSecurityServerOffer           *bool                        `yaml:"strict_security_server_offer"`
	EnableInitialRejectFallback         *bool                        `yaml:"enable_initial_reject_fallback"`
	FallbackIncludesServerParamsInSecCl *bool                        `yaml:"fallback_includes_server_params_in_sec_client"`
	RegisterPolicy                      IMSRegisterPolicyOverride    `yaml:"register_policy"`
}

type CarrierOverride struct {
	ID                            string                      `yaml:"id"`
	E911                          E911PolicyOverride          `yaml:"e911"`
	CustomEPDG                    string                      `yaml:"custom_epdg"`
	EPDGPort                      *int                        `yaml:"epdg_port"`
	APN                           string                      `yaml:"apn"`
	IPStackType                   string                      `yaml:"ip_stack"`
	DNSServer                     string                      `yaml:"dns_server"`
	AlgorithmPolicy               string                      `yaml:"algorithm_policy"`
	DeviceModel                   string                      `yaml:"device_model"`
	AKAChallengeMode              string                      `yaml:"aka_challenge_mode"`
	IKEIdentityMode               string                      `yaml:"ike_identity_mode"`
	AKAIdentityMode               string                      `yaml:"aka_identity_mode"`
	DeviceIdentityEnabled         *bool                       `yaml:"device_identity_enabled"`
	DeviceIdentityIMEI            string                      `yaml:"device_identity_imei"`
	NATKeepaliveSeconds           *int                        `yaml:"nat_keepalive_seconds"`
	DPDIntervalSeconds            *int                        `yaml:"dpd_interval_seconds"`
	EnableLegacyCiphers           *bool                       `yaml:"enable_legacy_ciphers"`
	AllowedLegacyCiphers          []string                    `yaml:"allowed_legacy_ciphers"`
	IKEProposals                  []string                    `yaml:"ike_proposals"`
	ESPProposals                  []string                    `yaml:"esp_proposals"`
	IMSDomain                     string                      `yaml:"ims_domain"`
	IMSRealm                      string                      `yaml:"ims_realm"`
	IMSRegistrar                  string                      `yaml:"ims_registrar"`
	IMSPCSCF                      string                      `yaml:"ims_pcscf"`
	IMSUserAgent                  string                      `yaml:"ims_user_agent"`
	IMSTransport                  string                      `yaml:"ims_transport"`
	IMSIdentitySource             string                      `yaml:"ims_identity_source"`
	IMSLocalPort                  *int                        `yaml:"ims_local_port"`
	IMSTCPKeepaliveSeconds        *int                        `yaml:"ims_tcp_keepalive_seconds"`
	IMSOptionsPingIntervalSeconds *int                        `yaml:"ims_options_ping_interval_seconds"`
	DPDKeepaliveIntervalSeconds   int                         `yaml:"dpd_keepalive_interval_seconds"`
	ReauthIntervalSeconds         int                         `yaml:"reauth_interval_seconds"`
	IMSRegisterTemplate           IMSRegisterTemplateOverride `yaml:"ims_register_template"`
	SMSRoutingMethod              string                      `yaml:"sms_routing_method"`
	SMSRoutingGW                  string                      `yaml:"sms_routing_gw"`
	ForceSMSCAuth                 *bool                       `yaml:"force_smsc_auth"`

	// Compatibility selectors used by the interim slice API.
	MCC, MNC, PresetID string              `yaml:"-"`
	IMS                IMSRegisterTemplate `yaml:"-"`
}

type CarrierPreset struct {
	ID, MCC, MNC                     string
	E911                             E911Policy `yaml:"e911"`
	CustomEPDG                       string
	EPDGPort                         *int
	APN, DNSServer                   string
	AlgorithmPolicy, DeviceModel     string
	AKAChallengeMode                 string
	IKEIdentityMode, AKAIdentityMode string
	DeviceIdentityIMEI               string
	DeviceIdentityEnabled            *bool
	NATKeepaliveSeconds              *int
	DPDIntervalSeconds               *int
	EnableLegacyCiphers              *bool
	AllowedLegacyCiphers             []string
	IKEProposals, ESPProposals       []string
	IMSDomain, IMSRealm              string
	IMSRegistrar, IMSPCSCF           string
	IMSUserAgent, IMSTransport       string
	IMSIdentitySource                string
	IMSLocalPort                     *int
	IMSTCPKeepaliveSeconds           *int
	IMSOptionsPingIntervalSeconds    *int
	DPDKeepaliveIntervalSeconds      int
	ReauthIntervalSeconds            int
	IMSRegisterTemplate              IMSRegisterTemplate
	IPStackType                      string `json:"ip_stack,omitempty" yaml:"ip_stack,omitempty"`
	SMSRoutingMethod, SMSRoutingGW   string
	ForceSMSCAuth                    *bool
}

type EffectiveCarrierConfig struct {
	MCC, MNC, PresetID, MatchedTemplate                string
	E911                                               E911Policy
	IPStackType, EPDGAddr, EPDGAddrSource              string
	EPDGPort                                           uint16
	APN, DNSServer                                     string
	NATKeepaliveSeconds, DPDIntervalSeconds            int
	AKAChallengeMode, IKEIdentityMode, AKAIdentityMode string
	IKEProposals, ESPProposals                         []string
	EnableLegacyCiphers                                bool
	AllowedLegacyCiphers                               []string
	AlgorithmPolicy, DeviceIdentityIMEI                string
	DeviceIdentityEnabled                              bool
	DeviceModel, IMSDomain, IMSRealm                   string
	IMSRegistrar, IMSPCSCF, IMSUserAgent               string
	IMSTransport, IMSIdentitySource                    string
	IMSLocalPort, IMSTCPKeepaliveSeconds               int
	IMSOptionsPingIntervalSeconds                      int
	DPDKeepaliveIntervalSeconds, ReauthIntervalSeconds int
	IMSRegisterTemplate                                IMSRegisterTemplate
	IMSRegisterPolicySource                            string
	SMSRoutingMethod, SMSRoutingGW                     string
	ForceSMSCAuth                                      bool

	IMS IMSRegisterTemplate
}

type CarrierMetadataPlan struct{ MCC, MNC, PresetID, MatchedTemplate string }
type E911Plan struct {
	Enabled                                      bool
	Provider, EntitlementURL, WebsheetHostPolicy string
}
type EPDGPlan struct {
	IPStackType, Addr, AddrSource string
	Port                          uint16
	APN, DNSServer                string
}
type IKEPlan struct {
	NATKeepaliveSeconds, DPDIntervalSeconds            int
	AKAChallengeMode, IKEIdentityMode, AKAIdentityMode string
	IKEProposals, ESPProposals                         []string
	EnableLegacyCiphers                                bool
	AllowedLegacyCiphers                               []string
	AlgorithmPolicy                                    string
	DPDKeepaliveIntervalSeconds, ReauthIntervalSeconds int
}
type IMSPlan struct {
	Domain, Realm, Registrar, PCSCF, UserAgent, Transport, IdentitySource string
	LocalPort, TCPKeepaliveSeconds, OptionsPingIntervalSeconds            int
	RegisterTemplate                                                      IMSRegisterTemplate
	RegisterPolicySource                                                  string
}
type SMSPlan struct {
	RoutingMethod, RoutingGW string
	ForceSMSCAuth            bool
}
type DeviceIdentityPlan struct {
	IdentityIMEI    string
	IdentityEnabled bool
	Model           string
}
type CarrierPlan struct {
	Metadata CarrierMetadataPlan
	E911     E911Plan
	EPDG     EPDGPlan
	IKE      IKEPlan
	IMS      IMSPlan
	SMS      SMSPlan
	Device   DeviceIdentityPlan
}
