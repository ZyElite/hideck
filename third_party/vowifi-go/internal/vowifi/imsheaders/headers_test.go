package imsheaders

import (
	"reflect"
	"testing"
)

func TestPreferredIdentityHeaderValue(t *testing.T) {
	tests := map[string]string{
		" sip:+447700900111@ims.example ": "<sip:+447700900111@ims.example>",
		"+447700900111@ims.example":       "<sip:+447700900111@ims.example>",
		"+447700900111":                   "<tel:+447700900111>",
		"private-user":                    "<sip:private-user>",
		"<tel:+447700900111>":             "<tel:+447700900111>",
		"":                                "",
	}
	for input, want := range tests {
		if got := PreferredIdentityHeaderValue(input); got != want {
			t.Errorf("PreferredIdentityHeaderValue(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRouteSet(t *testing.T) {
	want := []string{"<sip:service.example;lr>", "sip:outbound.example;lr"}
	if got := RouteSet(" <sip:service.example;lr> ", " sip:outbound.example;lr "); !reflect.DeepEqual(got, want) {
		t.Fatalf("RouteSet = %#v, want %#v", got, want)
	}
	if got := FirstRoute("", " sip:outbound.example;lr "); got != "sip:outbound.example;lr" {
		t.Fatalf("FirstRoute = %q", got)
	}
}

func TestSecAgreeProtectedHeaders(t *testing.T) {
	if headers := SecAgreeProtectedHeaders(" \t "); headers != nil {
		t.Fatalf("empty Security-Verify returned %#v", headers)
	}
	want := []Header{
		{Name: "Require", Value: "sec-agree"},
		{Name: "Proxy-Require", Value: "sec-agree"},
		{Name: "Security-Verify", Value: "ipsec-3gpp;alg=hmac-sha-1-96"},
	}
	if got := SecAgreeProtectedHeaders(" ipsec-3gpp;alg=hmac-sha-1-96 "); !reflect.DeepEqual(got, want) {
		t.Fatalf("SecAgreeProtectedHeaders = %#v, want %#v", got, want)
	}
}

func TestSingleIMSHeaders(t *testing.T) {
	if !HasSecurityVerify(" ipsec-3gpp ") || HasSecurityVerify(" ") {
		t.Fatal("HasSecurityVerify did not normalize whitespace")
	}
	if got := SecurityVerifyHeader(" verify "); got != (Header{Name: "Security-Verify", Value: "verify"}) {
		t.Fatalf("SecurityVerifyHeader = %#v", got)
	}
	if got := PAccessNetworkInfo(" wlan;country=GB "); got != "wlan;country=GB" {
		t.Fatalf("PAccessNetworkInfo = %q", got)
	}
	if got := PAccessNetworkInfo(" "); got != defaultPAccessNetworkInfo {
		t.Fatalf("default PAccessNetworkInfo = %q", got)
	}
}
