package carrier

import (
	"reflect"
	"strings"
	"testing"

	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
)

var (
	_ func(string, string) EffectiveCarrierConfig                = ResolveEffectiveCarrierConfig
	_ func(string) (string, int, bool, error)                    = LoadCarrierOverrides
	_ func(EffectiveCarrierConfigInput) EffectiveCarrierConfig   = ResolveEffectiveCarrierConfigCurrent
	_ func(string) (LoadResult, error)                           = LoadCarrierOverridesCurrent
	_ func(policy.IMSRegisterTemplate) IMSRegisterTemplate       = RegisterTemplateFromInternal
	_ func(policy.EffectiveCarrierConfig) EffectiveCarrierConfig = CarrierConfigFromInternal
)

func TestResolveEmbeddedCarrierPresets(t *testing.T) {
	tests := []struct{ mcc, mnc, id string }{
		{"530", "24", "2degrees_nz_53024"}, {"310", "280", "att_310280"},
		{"310", "410", "LycaMobile_310410"}, {"454", "000", "csl_454000"},
		{"234", "33", "CTEUK_23433"}, {"234", "10", "giffgaff_23410"}, {"234", "15", "vodafone_uk_23415"},
		{"262", "03", "O2_de_26203"}, {"262", "07", "O2_de_26207_alias"},
		{"530", "01", "one_nz_53001"}, {"530", "05", "spark_nz_53005"},
		{"228", "02", "sunrise_22802"}, {"454", "03", "three_hk_454003"},
		{"234", "20", "three_uk_234020"}, {"310", "240", "T-Mobile_240"},
		{"310", "260", "T-Mobile_260"}, {"204", "04", "vodafone_nl_20404"},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			config := ResolveEffectiveCarrierConfig(test.mcc, test.mnc)
			if config.PresetID != test.id {
				t.Fatalf("PresetID = %q, want %q", config.PresetID, test.id)
			}
			if err := ValidateEffectiveCarrierConfig(config); err != nil {
				t.Fatalf("ValidateEffectiveCarrierConfig() error = %v", err)
			}
		})
	}
}

func TestResolveRetainsOrderedProposalLists(t *testing.T) {
	generic := ResolveEffectiveCarrierConfig("001", "01")
	wantIKE := []string{
		"aes128-sha256-modp2048", "aes128-sha256-modp1024",
		"aes128-sha1-modp1024", "aes256-sha1-modp1024",
	}
	wantESP := []string{"aes256gcm16", "aes128gcm16", "aes128-sha256", "aes128-sha1"}
	if !reflect.DeepEqual(generic.IKEProposals, wantIKE) || !reflect.DeepEqual(generic.ESPProposals, wantESP) {
		t.Fatalf("generic proposals = IKE %v ESP %v", generic.IKEProposals, generic.ESPProposals)
	}
	tmobile := ResolveEffectiveCarrierConfig("310", "260")
	if want := []string{"aes128-sha256", "aes128-sha1"}; !reflect.DeepEqual(tmobile.ESPProposals, want) {
		t.Fatalf("T-Mobile ESP proposals = %v, want %v", tmobile.ESPProposals, want)
	}
	giffgaff := ResolveEffectiveCarrierConfig("234", "010")
	if !reflect.DeepEqual(giffgaff.IKEProposals, []string{IKEProposalAES256SHA512PRFSHA512MODP2048}) ||
		!reflect.DeepEqual(giffgaff.ESPProposals, []string{ESPProposalAES256SHA512}) {
		t.Fatalf("giffgaff proposals = IKE %v ESP %v", giffgaff.IKEProposals, giffgaff.ESPProposals)
	}
}

func TestCurrentInputAPIUsesRecoveredResolver(t *testing.T) {
	input := ResolveEffectiveCarrierConfigCurrent(EffectiveCarrierConfigInput{MCC: "234", MNC: "10"})
	legacy := ResolveEffectiveCarrierConfig("234", "10")
	if !reflect.DeepEqual(input, legacy) {
		t.Fatal("input compatibility result differs from recovered result")
	}
}

func TestResolvedCarrierSlicesAreDetached(t *testing.T) {
	first := ResolveEffectiveCarrierConfig("234", "10")
	first.IKEProposals[0] = "changed"
	first.IMSRegisterTemplate.ContactParamOrder[0] = "changed"
	first.IMS.SecurityClientMechanisms[0].Alg = "changed"
	first.IMS.RegisterPolicy.TemporaryStatusCodes[0] = 699
	second := ResolveEffectiveCarrierConfig("234", "10")
	if second.IKEProposals[0] == "changed" || second.IMSRegisterTemplate.ContactParamOrder[0] == "changed" ||
		second.IMS.SecurityClientMechanisms[0].Alg == "changed" ||
		second.IMS.RegisterPolicy.TemporaryStatusCodes[0] == 699 {
		t.Fatalf("resolved config exposed shared slices: %+v", second)
	}
}

func TestPublicConvertersDetachNestedSlices(t *testing.T) {
	internal := policy.ResolveEffectiveCarrierConfig("310", "280")
	external := CarrierConfigFromInternal(internal)
	external.IKEProposals[0] = "changed"
	external.IMSRegisterTemplate.ContactParamOrder[0] = "changed"
	external.IMSRegisterTemplate.SecurityClientMechanisms[0].Alg = "changed"
	external.IMSRegisterTemplate.RegisterPolicy.TemporaryStatusCodes[0] = 699
	if internal.IKEProposals[0] == "changed" || internal.IMSRegisterTemplate.ContactParamOrder[0] == "changed" ||
		internal.IMSRegisterTemplate.SecurityClientMechanisms[0].Alg == "changed" ||
		internal.IMSRegisterTemplate.RegisterPolicy.TemporaryStatusCodes[0] == 699 {
		t.Fatal("public conversion aliased internal slices")
	}
}

func TestValidateEffectiveCarrierConfigRejectsWireErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*EffectiveCarrierConfig)
		want   string
	}{
		{"invalid proposal", func(c *EffectiveCarrierConfig) { c.IKEProposals[0] = "unknown" }, "IKE proposals"},
		{"duplicate proposal", func(c *EffectiveCarrierConfig) { c.ESPProposals = append(c.ESPProposals, c.ESPProposals[0]) }, "duplicate ESP proposal"},
		{"invalid PLMN", func(c *EffectiveCarrierConfig) { c.MCC = "x" }, "invalid MCC"},
		{"invalid transport", func(c *EffectiveCarrierConfig) { c.IMS.Transport = "sctp" }, "unsupported IMS transport"},
		{"invalid SMS receiver", func(c *EffectiveCarrierConfig) { c.IMS.SMSReceiverTransport = "sctp" }, "unsupported SMS receiver"},
		{"invalid sec agree", func(c *EffectiveCarrierConfig) { c.IMS.SecAgreeMode = "sometimes" }, "unsupported sec-agree"},
		{"invalid status", func(c *EffectiveCarrierConfig) { c.IMS.RegisterPolicy.TemporaryStatusCodes = []int{700} }, "invalid REGISTER temporary status"},
		{"duplicate status", func(c *EffectiveCarrierConfig) { c.IMS.RegisterPolicy.ForbiddenStatusCodes = []int{403, 403} }, "duplicate REGISTER forbidden status"},
		{"negative retry", func(c *EffectiveCarrierConfig) { c.IMS.RegisterPolicy.TemporaryRetrySeconds = -1 }, "temporary retry"},
		{"invalid mechanism", func(c *EffectiveCarrierConfig) { c.IMS.SecurityClientMechanisms[0].Alg = "sha256" }, "Security-Client mechanism"},
		{"duplicate mechanism", func(c *EffectiveCarrierConfig) {
			c.IMS.SecurityClientMechanisms = append(c.IMS.SecurityClientMechanisms, c.IMS.SecurityClientMechanisms[0])
		}, "duplicate Security-Client"},
		{"invalid expiry", func(c *EffectiveCarrierConfig) { c.IMS.ExpiresSeconds = 0 }, "expiry must be positive"},
		{"unknown contact", func(c *EffectiveCarrierConfig) { c.IMS.ContactOrder = append(c.IMS.ContactOrder, "unknown") }, "unsupported IMS Contact"},
		{"negative reauth", func(c *EffectiveCarrierConfig) { c.ReauthIntervalSeconds = -1 }, "reauth interval"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := ResolveEffectiveCarrierConfig("234", "10")
			test.mutate(&config)
			if err := ValidateEffectiveCarrierConfig(config); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateEffectiveCarrierConfig() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateE911RequiresReachableEndpoint(t *testing.T) {
	config := ResolveEffectiveCarrierConfig("310", "280")
	config.E911.EntitlementURL = ""
	config.E911.Websheet = ""
	config.E911.EntitlementEndpoint = ""
	if err := ValidateEffectiveCarrierConfig(config); err == nil || !strings.Contains(err.Error(), "no entitlement endpoint") {
		t.Fatalf("ValidateEffectiveCarrierConfig() error = %v", err)
	}
}
