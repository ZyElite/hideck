package imscore

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
)

func enabledBoolPointer() *bool {
	value := true
	return &value
}

func disabledBoolPointer() *bool {
	value := false
	return &value
}

func TestIMSConfigOriginalFieldPrefix(t *testing.T) {
	want := []struct {
		name, tag string
	}{
		{"Enabled", "enabled"}, {"DeviceID", "device_id"}, {"PCSCF", "pcscf"},
		{"Registrar", "registrar"}, {"Domain", "domain"}, {"Realm", "realm"},
		{"IMPI", "impi"}, {"IMPU", "impu"}, {"CarrierPresetID", "-"},
		{"IMSRegisterTemplate", "-"}, {"IMSRegisterPolicySource", "-"},
		{"LocalAddr", "local_addr"}, {"LocalPort", "local_port"}, {"Transport", "transport"},
		{"UserAgent", "user_agent"}, {"PAccessNetworkInfo", "p_access_network_info"},
		{"CellularNetworkInfo", "cellular_network_info"}, {"SIPInstance", "sip_instance"},
		{"IcsiRef", "icsi_ref"}, {"TCPKeepaliveSeconds", "tcp_keepalive_seconds"},
		{"OptionsPingIntervalSeconds", "options_ping_interval_seconds"}, {"IMScore", "imscore"},
		{"EnableIPSec3GPP", "-"}, {"SMSRoutingMethod", "-"}, {"SMSRoutingGW", "-"},
		{"ForceSMSCAuth", "-"},
	}
	typeOfConfig := reflect.TypeOf(IMSConfig{})
	for index, expected := range want {
		field := typeOfConfig.Field(index)
		if field.Name != expected.name || field.Tag.Get("mapstructure") != expected.tag {
			t.Fatalf("field %d = %s %q, want %s %q", index, field.Name, field.Tag.Get("mapstructure"), expected.name, expected.tag)
		}
	}
	if field, _ := typeOfConfig.FieldByName("IMPU"); field.Type.Kind() != reflect.String {
		t.Fatalf("IMPU type = %s, want string", field.Type)
	}
}

func TestIMSConfigIPSecDefaultAndOverride(t *testing.T) {
	var cfg IMSConfig
	if !cfg.IPSec3GPPEnabled() {
		t.Fatal("nil override must enable 3GPP IPsec")
	}
	cfg.SetEnableIPSec3GPP(false)
	if cfg.IPSec3GPPEnabled() {
		t.Fatal("explicit false override was ignored")
	}
	copyOfConfig := cfg
	copyOfConfig.SetEnableIPSec3GPP(true)
	if cfg.IPSec3GPPEnabled() || !copyOfConfig.IPSec3GPPEnabled() {
		t.Fatal("SetEnableIPSec3GPP reused mutable pointer state")
	}
	var nilConfig *IMSConfig
	nilConfig.SetEnableIPSec3GPP(true)
}

func TestIMSConfigSMSReceiverTransport(t *testing.T) {
	for value, want := range map[string]string{"": "dual", "both": "dual", "tcp": "tcp", "off": "none"} {
		cfg := IMSConfig{IMScore: IMScoreConfig{ReceiverTransport: value}}
		if got := cfg.SMSReceiverTransport(); got != want {
			t.Fatalf("SMSReceiverTransport(%q) = %q, want %q", value, got, want)
		}
	}
}

type identityProviderStub struct {
	identity profile.Identity
	err      error
}

func (stub identityProviderStub) GetISIMIdentity() (profile.Identity, error) {
	return stub.identity, stub.err
}

func TestResolveIMSIdentitySource(t *testing.T) {
	provider := identityProviderStub{identity: profile.Identity{
		IMPI: " user@ims.example ", IMPU: []string{"", " sip:user@ims.example "},
	}}
	result, err := ResolveIMSIdentitySource("isim", provider)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || result.IMPI != "user@ims.example" || result.Domain != "ims.example" || result.AKAAppPreference != profile.AKAAppISIMStrict {
		t.Fatalf("resolved identity = %+v", result)
	}

	wantErr := errors.New("read ISIM")
	if result, err = ResolveIMSIdentitySource("auto", identityProviderStub{err: wantErr}); err != nil || result.Applied {
		t.Fatalf("auto fallback = %+v, %v", result, err)
	}
	if _, err = ResolveIMSIdentitySource("isim", nil); err == nil ||
		err.Error() != "IMSIdentitySource=isim 但 provider 不支持 ISIM 身份读取" {
		t.Fatalf("strict ISIM provider error = %v", err)
	}
}

func TestIMSIdentityErrorsMatchOriginal(t *testing.T) {
	tests := []struct {
		name     string
		identity profile.Identity
		want     string
	}{
		{name: "IMPI", identity: profile.Identity{IMPU: []string{"sip:user@ims.example"}}, want: "ISIM 身份不完整: 缺少 IMPI"},
		{name: "IMPU", identity: profile.Identity{IMPI: "user@ims.example"}, want: "ISIM 身份不完整: 缺少 IMPU"},
		{name: "DOMAIN", identity: profile.Identity{IMPI: "user", IMPU: []string{"sip:user"}}, want: "ISIM 身份不完整: 缺少 DOMAIN"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ResolveIMSIdentitySource("isim", identityProviderStub{identity: test.identity})
			if err == nil || err.Error() != test.want {
				t.Fatalf("ResolveIMSIdentitySource() error = %v, want %q", err, test.want)
			}
		})
	}
	if err := ApplyResolvedIMSIdentityToConfig(nil, profile.IMSIdentityResult{}, "234"); err == nil || err.Error() != "IMSConfig 为空" {
		t.Fatalf("nil IMSConfig error = %v", err)
	}
	incomplete := profile.IMSIdentityResult{Applied: true, IMPI: "user"}
	if err := ApplyResolvedIMSIdentityToConfig(&IMSConfig{}, incomplete, "234"); err == nil ||
		err.Error() != "ISIM 身份不完整: 缺少 IMPI/IMPU/DOMAIN" {
		t.Fatalf("incomplete applied identity error = %v", err)
	}
}

func TestApplyResolvedIMSIdentityAndAORHelpers(t *testing.T) {
	cfg := &IMSConfig{IMPU: "sip:old@ims.example", IMPUs: []string{"sip:extra@ims.example"}}
	identity := profile.IMSIdentityResult{
		Applied: true, ActualSource: "isim", IMPI: "user@ims.example",
		IMPU: "sip:user@ims.example", Domain: "sip:ims.example;transport=tcp",
	}
	if err := ApplyResolvedIMSIdentityToConfig(cfg, identity, "234"); err != nil {
		t.Fatal(err)
	}
	if cfg.IMPU != identity.IMPU || len(cfg.IMPUs) != 0 || cfg.Realm != "ims.example" {
		t.Fatalf("applied config = %+v", cfg)
	}
	if !strings.Contains(cfg.PAccessNetworkInfo, ";country=GB") {
		t.Fatalf("PANI = %q", cfg.PAccessNetworkInfo)
	}
	if got := pickAOR(*cfg); got != identity.IMPU {
		t.Fatalf("pickAOR = %q", got)
	}
	if got := preferredPublicAOR(" ", "sip:associated@example", "sip:fallback@example"); got != "sip:associated@example" {
		t.Fatalf("preferredPublicAOR = %q", got)
	}
}

func TestShouldSendTGSuccess(t *testing.T) {
	if !shouldSendTGSuccess(nil) || !shouldSendTGSuccess(context.Background()) {
		t.Fatal("default context suppressed TG success")
	}
	if shouldSendTGSuccess(withSuppressTGSuccess(context.Background(), true)) {
		t.Fatal("suppression context was ignored")
	}
	if shouldSendTGSuccess(context.WithValue(context.Background(), 0, true)) {
		t.Fatal("original integer context key was ignored")
	}
}

func TestBuildIMSConfigUsesCarrierPlan(t *testing.T) {
	plan := policy.CarrierPlan{
		Metadata: policy.CarrierMetadataPlan{PresetID: "carrier-1"},
		IMS: policy.IMSPlan{
			Realm: "realm.example", PCSCF: "pcscf.example", Registrar: "registrar.example",
			Transport: "TCP", LocalPort: 5070, TCPKeepaliveSeconds: 31,
			OptionsPingIntervalSeconds: 47, RegisterPolicySource: "preset",
			RegisterTemplate: policy.IMSRegisterTemplate{SMSReceiverTransport: "udp", SecAgreeMode: "disabled"},
		},
		SMS: policy.SMSPlan{RoutingMethod: "tel", RoutingGW: "sms.example", ForceSMSCAuth: true},
	}
	cfg := BuildIMSConfigFromCarrier(
		"dev-1", "234102356143376", "urn:gsma:imei:35693803-564380-9",
		"234", "10", "", "test-agent", "10.0.0.2", plan,
	)
	if cfg.Domain != "ims.mnc010.mcc234.3gppnetwork.org" || cfg.Transport != "tcp" || cfg.LocalPort != 5070 {
		t.Fatalf("carrier config = %+v", cfg)
	}
	if cfg.IPSec3GPPEnabled() || cfg.SMSReceiverTransport() != "udp" || cfg.SMSRoutingMethod != "tel_uri_smsc" {
		t.Fatalf("carrier policy projection = %+v", cfg)
	}
}
