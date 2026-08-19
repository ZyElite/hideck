package policy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestDefaultIMSRegisterTemplate(t *testing.T) {
	template := DefaultIMSRegisterTemplate()
	if template.ID != "defaultIMSRegisterTemplate" || template.Expires != 600000 ||
		template.SecAgreeMode != "auto" || template.RegisterPolicy.TemporaryRetrySeconds != 60 {
		t.Fatalf("default template = %+v", template)
	}
	if len(template.SecurityClientMechanisms) != 6 || template.SecurityClientMechanisms[0].Alg != "hmac-md5-96" {
		t.Fatalf("security mechanisms = %+v", template.SecurityClientMechanisms)
	}
	if template.SMSReceiverTransport != "" || NormalizeIMSRegisterTemplate(template).SMSReceiverTransport != "dual" {
		t.Fatalf("receiver defaults = raw %q normalized %q", template.SMSReceiverTransport, NormalizeIMSRegisterTemplate(template).SMSReceiverTransport)
	}
}

func TestNormalizeIMSRegisterPolicy(t *testing.T) {
	input := IMSRegisterPolicy{
		ID: " custom ", TemporaryStatusCodes: []int{99, 503, 503, 700},
		ForbiddenStatusCodes: []int{}, InitialRejectFallbackStatusCodes: []int{400, 400},
		TemporaryRetrySeconds: 0,
	}
	got := NormalizeIMSRegisterPolicy(input)
	if got.ID != "custom" || !reflect.DeepEqual(got.TemporaryStatusCodes, []int{503}) ||
		len(got.ForbiddenStatusCodes) != 0 || !reflect.DeepEqual(got.InitialRejectFallbackStatusCodes, []int{400}) ||
		got.TemporaryRetrySeconds != 0 {
		t.Fatalf("NormalizeIMSRegisterPolicy() = %+v", got)
	}
	if got := NormalizeIMSRegisterPolicy(IMSRegisterPolicy{}); !isZeroIMSRegisterPolicy(got) {
		t.Fatalf("zero policy changed: %+v", got)
	}
}

func TestFallbackIMSRegisterTemplate(t *testing.T) {
	template := DefaultIMSRegisterTemplate()
	template.SecurityClientIncludesServerParams = true
	template.FallbackIncludesServerParamsInSecCl = false
	fallback := FallbackIMSRegisterTemplate(template)
	if fallback.ID != "minimal_generic" || fallback.EnableInitialRejectFallback ||
		fallback.SecurityClientIncludesServerParams {
		t.Fatalf("FallbackIMSRegisterTemplate = %+v", fallback)
	}
	template.FallbackIncludesServerParamsInSecCl = true
	if fallback := FallbackIMSRegisterTemplate(template); !fallback.SecurityClientIncludesServerParams {
		t.Fatal("fallback server-parameter policy was not projected")
	}
}

func TestNormalizeCarrierValues(t *testing.T) {
	if got := NormalizeIMSDomain(" SIPS:IMS.Example.COM;transport=tcp "); got != "IMS.Example.COM" {
		t.Fatalf("NormalizeIMSDomain() = %q", got)
	}
	if got := NormalizeIMSIdentitySource("usim"); got != "derived" {
		t.Fatalf("identity = %q", got)
	}
	if got := NormalizeIMSTransport("tls"); got != "auto" {
		t.Fatalf("transport = %q", got)
	}
	if got := NormalizeSMSReceiverTransport("both"); got != "dual" {
		t.Fatalf("receiver = %q", got)
	}
	if got := NormalizeSMSRoutingMethod("ipsmgw"); got != "ip_sm_gw" {
		t.Fatalf("routing = %q", got)
	}
	if got := NormalizeCarrierDNSServer("2001:db8::1"); got != "[2001:db8::1]:53" {
		t.Fatalf("DNS = %q", got)
	}
}

func TestVoWiFiBlocklistAndErrorChain(t *testing.T) {
	for _, mcc := range []string{"460", "461"} {
		if !IsVoWiFiBlockedMCC(mcc) {
			t.Fatalf("MCC %s should be blocked", mcc)
		}
	}
	if IsVoWiFiBlockedMCC("46") || IsVoWiFiBlockedMCC("310") {
		t.Fatal("invalid or US MCC blocked")
	}
	err := NewVoWiFiBlockedMCCError("460")
	if !errors.Is(err, ErrVoWiFiPolicyBlocked) || !IsVoWiFiPolicyBlockedError(err) {
		t.Fatalf("error chain = %v", err)
	}
}

func TestResolveEmbeddedCarrierPresets(t *testing.T) {
	giffgaff := ResolveEffectiveCarrierConfig("234", "10")
	if giffgaff.PresetID != "giffgaff_23410" || giffgaff.DeviceModel != "rmx3366" ||
		giffgaff.ReauthIntervalSeconds != 0 || giffgaff.EPDGAddrSource != "standard" {
		t.Fatalf("giffgaff = %+v", giffgaff)
	}
	if !reflect.DeepEqual(giffgaff.IKEProposals, []string{"aes256-sha512-prfsha512-modp2048"}) {
		t.Fatalf("giffgaff IKE = %+v", giffgaff.IKEProposals)
	}
	vodafoneUK := ResolveEffectiveCarrierConfig("234", "15")
	if vodafoneUK.PresetID != "vodafone_uk_23415" || vodafoneUK.DeviceModel != "rmx3366" ||
		vodafoneUK.EPDGAddr != "epdg.epc.mnc015.mcc234.pub.3gppnetwork.org" ||
		vodafoneUK.EPDGAddrSource != "standard" ||
		vodafoneUK.IMSRegisterTemplate.ID != "vodafone_uk_23415" {
		t.Fatalf("vodafone uk = %+v", vodafoneUK)
	}
	if !strings.Contains(vodafoneUK.IMSRegisterTemplate.ICSIRef, "ims.icsi.sms") {
		t.Fatalf("vodafone uk ICSI missing SMS: %q", vodafoneUK.IMSRegisterTemplate.ICSIRef)
	}
	if !reflect.DeepEqual(vodafoneUK.IMSRegisterTemplate.ContactParamOrder[:5], []string{
		"access_type", "sip_instance", "audio", "smsip", "icsi_ref",
	}) {
		t.Fatalf("vodafone uk contact order = %v", vodafoneUK.IMSRegisterTemplate.ContactParamOrder)
	}
	lebaraUK := ResolveEffectiveCarrierConfig("234", "87")
	if lebaraUK.PresetID != "lebara_uk_23487" || lebaraUK.DeviceModel != "rmx3366" ||
		lebaraUK.EPDGAddr != "epdg.epc.mnc087.mcc234.pub.3gppnetwork.org" ||
		lebaraUK.EPDGAddrSource != "standard" ||
		lebaraUK.IMSRegisterTemplate.ID != "lebara_uk_23487" {
		t.Fatalf("lebara uk = %+v", lebaraUK)
	}
	if !strings.Contains(lebaraUK.IMSRegisterTemplate.ICSIRef, "ims.icsi.sms") {
		t.Fatalf("lebara uk ICSI missing SMS: %q", lebaraUK.IMSRegisterTemplate.ICSIRef)
	}
	if !reflect.DeepEqual(lebaraUK.IMSRegisterTemplate.ContactParamOrder[:5], []string{
		"access_type", "sip_instance", "audio", "smsip", "icsi_ref",
	}) {
		t.Fatalf("lebara uk contact order = %v", lebaraUK.IMSRegisterTemplate.ContactParamOrder)
	}
	if vodafoneNL := ResolveEffectiveCarrierConfig("204", "04"); vodafoneNL.PresetID != "vodafone_nl_20404" {
		t.Fatalf("vodafone nl stolen by lebara: %+v", vodafoneNL)
	}
	att := ResolveEffectiveCarrierConfig("310", "280")
	if att.PresetID != "att_310280" || att.EPDGAddr != "epdg.epc.att.net" ||
		att.IMSRegisterTemplate.RegisterPolicy.ID != "att_main" || att.IMSRegisterPolicySource != "preset" {
		t.Fatalf("AT&T = %+v", att)
	}
}

func TestCarrierOverridesAreAtomicAndClearable(t *testing.T) {
	ClearCarrierOverrides()
	t.Cleanup(ClearCarrierOverrides)
	port, disabled := 4500, false
	err := SetCarrierOverrides(map[string]CarrierOverride{
		"310-260": {
			ID: "external", CustomEPDG: "epdg.example.com", EPDGPort: &port,
			E911: E911PolicyOverride{Enabled: &disabled}, IMSDomain: "ims.example.com",
		},
	})
	if err != nil {
		t.Fatalf("SetCarrierOverrides() error = %v", err)
	}
	cfg := ResolveEffectiveCarrierConfig("310", "260")
	if cfg.PresetID != "external" || cfg.EPDGAddr != "epdg.example.com" || cfg.EPDGPort != 4500 ||
		cfg.IMSDomain != "ims.example.com" {
		t.Fatalf("override config = %+v", cfg)
	}
	if err := SetCarrierOverrides(map[string]CarrierOverride{"bad": {}}); err == nil {
		t.Fatal("invalid replacement should fail")
	}
	if got := ResolveEffectiveCarrierConfig("310", "260"); got.PresetID != "external" {
		t.Fatal("failed replacement mutated active overrides")
	}
	ClearCarrierOverrides()
	if got := ResolveEffectiveCarrierConfig("310", "260"); got.PresetID == "external" {
		t.Fatal("ClearCarrierOverrides() retained override")
	}
}

func TestCarrierOverrideStoreDeepCopiesPointers(t *testing.T) {
	ClearCarrierOverrides()
	t.Cleanup(ClearCarrierOverrides)
	port := 4500
	values := map[string]CarrierOverride{"23410": {EPDGPort: &port}}
	if err := SetCarrierOverrides(values); err != nil {
		t.Fatal(err)
	}
	port = 1
	if got := ResolveEffectiveCarrierConfig("234", "10").EPDGPort; got != 4500 {
		t.Fatalf("stored pointer was aliased: %d", got)
	}
}

func TestCarrierOverrideStoreConcurrentReplacementAndResolution(t *testing.T) {
	ClearCarrierOverrides()
	t.Cleanup(ClearCarrierOverrides)
	const iterations = 100
	var workers sync.WaitGroup
	errCh := make(chan error, 1)
	workers.Add(2)
	go func() {
		defer workers.Done()
		for index := 0; index < iterations; index++ {
			err := SetCarrierOverrides(map[string]CarrierOverride{
				"23410": {ID: "concurrent", IKEProposals: []string{"aes128-sha256-modp2048"}},
			})
			if err != nil {
				errCh <- err
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		for index := 0; index < iterations; index++ {
			config := ResolveEffectiveCarrierConfig("234", "10")
			if len(config.IKEProposals) > 0 {
				config.IKEProposals[0] = "caller mutation"
			}
		}
	}()
	workers.Wait()
	close(errCh)
	if err := <-errCh; err != nil {
		t.Fatalf("SetCarrierOverrides() error = %v", err)
	}
	config := ResolveEffectiveCarrierConfig("234", "10")
	if len(config.IKEProposals) == 0 || config.IKEProposals[0] == "caller mutation" {
		t.Fatalf("concurrent resolution exposed stored slices: %+v", config.IKEProposals)
	}
}

func TestLoadCarrierOverridesYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "carrier_overrides.yaml")
	data := []byte("carrier_overrides:\n  23410:\n    id: local\n    custom_epdg: epdg.local\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	values, err := LoadCarrierOverridesFile(path)
	if err != nil {
		t.Fatalf("LoadCarrierOverridesFile() error = %v", err)
	}
	if values["234010"].ID != "local" {
		t.Fatalf("values = %+v", values)
	}
	resolved, count, missing, err := LoadAndSetCarrierOverridesFile(path)
	if err != nil || resolved != path || count != 1 || missing {
		t.Fatalf("LoadAndSetCarrierOverridesFile() = %q %d %t %v", resolved, count, missing, err)
	}
}

func TestCarrierPlanRoundTripAndSliceIsolation(t *testing.T) {
	config := ResolveEffectiveCarrierConfig("234", "10")
	plan := CarrierPlanFromEffectiveConfig(config)
	back := EffectiveCarrierConfigFromCarrierPlan(plan)
	if back.MCC != config.MCC || back.PresetID != config.PresetID || !reflect.DeepEqual(back.IKEProposals, config.IKEProposals) {
		t.Fatalf("round trip = %+v", back)
	}
	plan.IKE.IKEProposals[0] = "changed"
	if config.IKEProposals[0] == "changed" {
		t.Fatal("plan aliases effective config")
	}
}

func TestIMSRegisterTemplateJSONCompatibility(t *testing.T) {
	var recovered IMSRegisterTemplate
	if err := json.Unmarshal([]byte(`{"Domain":"ims.example","RegisterPolicy":{"ID":"temporary"},"SecAgreeMode":"required"}`), &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered.Domain != "ims.example" || recovered.RegisterPolicy.ID != "temporary" || recovered.SecAgreeMode != "required" {
		t.Fatalf("recovered JSON = %+v", recovered)
	}
	var interim IMSRegisterTemplate
	if err := json.Unmarshal([]byte(`{"RegisterPolicy":"manual","SecAgreeMode":true}`), &interim); err != nil {
		t.Fatal(err)
	}
	if interim.RegisterPolicyMode != "manual" || !interim.SecAgreeEnabled {
		t.Fatalf("interim JSON = %+v", interim)
	}
}
