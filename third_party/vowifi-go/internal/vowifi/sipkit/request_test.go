package sipkit

import (
	"strings"
	"testing"

	"github.com/emiago/sipgo/sip"
)

func TestBuildIMSRequestComplete(t *testing.T) {
	recipient := mustURI(t, "sip:+44123@ims.example;user=phone")
	from := mustURI(t, "sip:+44999@ims.example")
	to := mustURI(t, "sip:+44123@ims.example")
	request, err := BuildIMSRequest(sip.INVITE, recipient, IMSRequestOptions{
		Destination: "192.0.2.1:5060", Transport: "tcp", ViaHost: "2001:db8::1", ViaPort: 5062,
		Branch: "z9hG4bK-test", FromURI: from, FromTag: "local", ToURI: to,
		CallID: "call-1", CSeq: 42, Routes: []string{"<sip:pcscf.example;lr>"},
		Kind: RequestKindOutOfDialog, SecurityMode: "enabled", AddRPort: true, AddAlias: true,
		AddPreferredService: true, PreferredService: "urn:service", AddAcceptContact: true,
		AcceptContact: "*;+g.test", AddSupported: true, Supported: "100rel", AddAllow: true,
		Allow: "INVITE, ACK", AddUserAgent: true, UserAgent: "VoHive-Test",
		PreferredIdentity: "<sip:+44999@ims.example>", SecurityVerify: "ipsec-3gpp;spi-c=1",
		Runtime:     IMSRuntimeSnapshot{PAccessNetworkInfo: "3GPP-E-UTRAN", LocalAddr: "[2001:db8::1]:5062"},
		ContentType: "application/sdp", Body: []byte("v=0\r\n"),
	})
	if err != nil {
		t.Fatalf("BuildIMSRequest: %v", err)
	}
	wire := request.String()
	for _, want := range []string{
		"INVITE sip:+44123@ims.example;user=phone;transport=tcp SIP/2.0",
		"Via: SIP/2.0/TCP [2001:db8::1]:5062;rport;branch=z9hG4bK-test;alias",
		"Route: <sip:pcscf.example;lr>", "From: <sip:+44999@ims.example>;tag=local",
		"Call-ID: call-1", "CSeq: 42 INVITE", "Require: sec-agree",
		"P-Access-Network-Info: 3GPP-E-UTRAN", "Security-Verify: ipsec-3gpp;spi-c=1",
		"Content-Length: 5",
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("request missing %q:\n%s", want, wire)
		}
	}
}

func TestBuildIMSRequestUsesRuntimeRouteAndTransport(t *testing.T) {
	request, err := BuildIMSRequest(sip.OPTIONS, mustURI(t, "sip:service@ims.example"), IMSRequestOptions{
		FromURI: mustURI(t, "sip:user@ims.example"), ToURI: mustURI(t, "sip:service@ims.example"),
		CallID: "call-2", CSeq: 1, SecurityMode: "disabled", RequireRoute: true,
		Runtime: IMSRuntimeSnapshot{ServiceRoute: "<sip:route.example;lr>", LocalAddr: "10.0.0.2:5070", Transport: "udp"},
	})
	if err != nil {
		t.Fatalf("BuildIMSRequest: %v", err)
	}
	if request.Transport() != "UDP" || request.Via().Host != "10.0.0.2" || request.Via().Port != 5070 {
		t.Fatalf("transport/Via = %q %+v", request.Transport(), request.Via())
	}
	if got := FirstHeaderValue(request, "Route", true); got != "<sip:route.example;lr>" {
		t.Fatalf("Route = %q", got)
	}
	if request.GetHeaders("Require") != nil {
		t.Fatal("disabled security emitted Require")
	}
}

func TestBuildIMSRequestRejectsIncompleteTransaction(t *testing.T) {
	options := IMSRequestOptions{ViaHost: "127.0.0.1", FromURI: mustURI(t, "sip:a@b"), ToURI: mustURI(t, "sip:c@d")}
	if _, err := BuildIMSRequest(sip.INVITE, mustURI(t, "sip:c@d"), options); err == nil {
		t.Fatal("missing Call-ID accepted")
	}
	options.CallID = "call"
	if _, err := BuildIMSRequest(sip.INVITE, mustURI(t, "sip:c@d"), options); err == nil {
		t.Fatal("missing CSeq accepted")
	}
}

func TestBuildCancelFromInviteCopiesTransaction(t *testing.T) {
	invite := completeInvite(t)
	cancel, err := BuildCancelFromInvite(invite)
	if err != nil {
		t.Fatalf("BuildCancelFromInvite: %v", err)
	}
	if cancel.Method != sip.CANCEL || cancel.CSeq().MethodName != sip.CANCEL {
		t.Fatalf("CANCEL method/CSeq = %s/%s", cancel.Method, cancel.CSeq().MethodName)
	}
	if cancel.Via().Value() != invite.Via().Value() || len(cancel.GetHeaders("Route")) != 1 {
		t.Fatal("CANCEL transaction route mismatch")
	}
	invite.Via().Params.Add("changed", "yes")
	if cancel.Via().Params.Has("changed") {
		t.Fatal("CANCEL Via aliases INVITE Via params")
	}
	if cancel.Source() != invite.Source() || cancel.Destination() != invite.Destination() {
		t.Fatal("CANCEL address metadata mismatch")
	}
}

func completeInvite(t *testing.T) *sip.Request {
	request, err := BuildIMSRequest(sip.INVITE, mustURI(t, "sip:b@example.com"), IMSRequestOptions{
		Transport: "udp", ViaHost: "127.0.0.1", ViaPort: 5060, Branch: "z9hG4bK-cancel",
		FromURI: mustURI(t, "sip:a@example.com"), FromTag: "from", ToURI: mustURI(t, "sip:b@example.com"),
		CallID: "cancel-call", CSeq: 9, Routes: []string{"<sip:route.example;lr>"}, SecurityMode: "disabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	request.SetSource("127.0.0.1:5060")
	request.SetDestination("127.0.0.2:5060")
	return request
}

func mustURI(t *testing.T, value string) sip.Uri {
	t.Helper()
	uri, err := parseURIValue(value)
	if err != nil {
		t.Fatalf("parse URI %q: %v", value, err)
	}
	return *uri
}
