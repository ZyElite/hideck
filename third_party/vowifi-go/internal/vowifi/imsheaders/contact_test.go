package imsheaders

import (
	"reflect"
	"testing"
)

func TestFormatHostForSIP(t *testing.T) {
	tests := map[string]string{
		"  [2001:db8::1]  ": "[2001:db8::1]",
		"example.com":       "example.com",
		"":                  "",
	}
	for input, want := range tests {
		if got := formatHostForSIP(input); got != want {
			t.Errorf("formatHostForSIP(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeSipInstance(t *testing.T) {
	tests := map[string]string{
		"356938035643809":                    "<urn:gsma:imei:35693803-564380-9>",
		"35-693803-564380":                   "<urn:gsma:imei:35693803-564380-9>",
		"urn:gsma:imei:35693803-564380-9":    "<urn:gsma:imei:35693803-564380-9>",
		"<urn:uuid:3d2f61a0-663d-4fd4-a8f0>": "<urn:uuid:3d2f61a0-663d-4fd4-a8f0>",
		"urn:uuid:abc":                       "<urn:uuid:abc>",
		"":                                   "",
	}
	for input, want := range tests {
		if got := NormalizeSipInstance(input); got != want {
			t.Errorf("NormalizeSipInstance(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestContactURIWithOptions(t *testing.T) {
	options := ContactOptions{
		ContactID: "234102356143376", LocalAddr: "2001:db8::10",
		LocalPortC: 5060, LocalPortS: 41001,
	}
	if got, want := ContactPort(options), 41001; got != want {
		t.Fatalf("ContactPort = %d, want %d", got, want)
	}
	if got, want := baseContactURI(options), "sip:234102356143376@[2001:db8::10]"; got != want {
		t.Fatalf("ContactURI = %q, want %q", got, want)
	}
	want := "sip:234102356143376@[2001:db8::10]:41001;transport=tcp"
	if got := ContactURI(options, " TCP "); got != want {
		t.Fatalf("ContactURI = %q, want %q", got, want)
	}
	if got := ContactURIWithOptions(options, " TCP ", true); got != want {
		t.Fatalf("ContactURIWithOptions = %q, want %q", got, want)
	}
}

func TestContactParamsRecoveredOrderAndValues(t *testing.T) {
	options := ContactOptions{
		AccessType: " IEEE-802.11 ", IMEI: "356938035643809",
		ContactParamOrder: []string{
			"access_type", "sip_instance", "audio", "smsip", "reg_id",
			"icsi_ref", "mid_call", "srvcc_alerting",
			"ps2cs_srvcc_orig_pre_alerting", "UNKNOWN", " audio ",
		},
	}
	want := []ContactParam{
		{Name: "+g.3gpp.accesstype", Value: `"IEEE-802.11"`},
		{Name: "+sip.instance", Value: `"<urn:gsma:imei:35693803-564380-9>"`},
		{Name: "audio"}, {Name: "+g.3gpp.smsip"},
		{Name: "reg-id", Value: "1"},
		{Name: "+g.3gpp.icsi-ref", Value: `"` + defaultICSIRef + `"`},
		{Name: "+g.3gpp.mid-call"}, {Name: "+g.3gpp.srvcc-alerting"},
		{Name: "+g.3gpp.ps2cs-srvcc-orig-pre-alerting"},
	}
	if got := ContactParams(options); !reflect.DeepEqual(got, want) {
		t.Fatalf("ContactParams = %#v\nwant          = %#v", got, want)
	}
}

func TestIMSContactURICompatibilitySurface(t *testing.T) {
	got := IMSContactURI("sip:user@192.0.2.10:5060", IMSContactOptions{
		Transport: "UDP", AccessType: "IEEE-802.11", Instance: "356938035643809",
		ICSIRef:    `"custom-icsi"`,
		ParamOrder: []string{"access_type", "sip_instance", "icsi_ref", "smsip"},
	})
	want := `<sip:user@192.0.2.10:5060;transport=udp>` +
		`;+g.3gpp.accesstype="IEEE-802.11"` +
		`;+sip.instance="<urn:gsma:imei:35693803-564380-9>"` +
		`;+g.3gpp.icsi-ref="custom-icsi";+g.3gpp.smsip`
	if got != want {
		t.Fatalf("IMSContactURI = %q\nwant          = %q", got, want)
	}
}
