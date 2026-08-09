package profile

import (
	"strings"
	"testing"

	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
)

func TestNormalizeAndBuild(t *testing.T) {
	got, err := Build(Profile{
		IMSI: " 234102356143376 ", MCC: " 234 ", MNC: " 10 ",
		IMEI: " 123 ", SMSC: " +447785016005 ", IMSDomain: " sips:IMS.Example;transport=tcp ",
	}, " fallback-agent ")
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if got.IMSI != "234102356143376" || got.MCC != "234" || got.MNC != "10" {
		t.Fatalf("Build() identity = %+v", got)
	}
	if got.UserAgent != "fallback-agent" || got.IMSDomain != "IMS.Example" {
		t.Fatalf("Build() defaults = %+v", got)
	}

	defaulted, err := Build(Profile{IMSI: "23410", MCC: "234", MNC: "10"}, "")
	if err != nil {
		t.Fatalf("Build(defaults) error = %v", err)
	}
	if defaulted.UserAgent != defaultUserAgent || defaulted.IMSDomain != "ims.mnc010.mcc234.3gppnetwork.org" {
		t.Fatalf("Build(defaults) = %+v", defaulted)
	}
}

func TestBuildRejectsMissingIdentity(t *testing.T) {
	if _, err := Build(Profile{MCC: "234", MNC: "10"}, ""); err == nil || err.Error() != "无法获取 IMSI" {
		t.Fatalf("missing IMSI error = %v", err)
	}
	if _, err := Build(Profile{IMSI: "23410"}, ""); err == nil || !strings.Contains(err.Error(), "23410") {
		t.Fatalf("missing PLMN error = %v", err)
	}
}

func TestResolveIdentityIMEIPriority(t *testing.T) {
	plan := policy.CarrierPlan{Device: policy.DeviceIdentityPlan{
		Model: "rmx3366", IdentityIMEI: "configured",
	}}
	generated, source := ResolveIdentityIMEI("234102356143376", "input", "iphone15,4", plan)
	if !strings.HasPrefix(generated, "86034905") || source != "carrier_device_model" {
		t.Fatalf("model IMEI = %q, source = %q", generated, source)
	}

	plan.Device.Model = "unknown"
	if got, gotSource := ResolveIdentityIMEI("seed", "input", "iphone15,4", plan); got != "configured" || gotSource != "device_identity_imei" {
		t.Fatalf("configured IMEI = %q, source = %q", got, gotSource)
	}
	plan.Device.IdentityIMEI = ""
	if got, gotSource := ResolveIdentityIMEI("seed", " input ", "iphone15,4", plan); got != "input" || gotSource != "input" {
		t.Fatalf("input IMEI = %q, source = %q", got, gotSource)
	}
	if got, gotSource := ResolveIdentityIMEI("seed", "", "iphone15,4", plan); got == "" || gotSource != "user_agent" {
		t.Fatalf("user-agent IMEI = %q, source = %q", got, gotSource)
	}
}
