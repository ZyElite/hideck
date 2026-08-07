package imscore

import (
	"strings"
	"testing"
)

func TestFirstSIPHeaderURI(t *testing.T) {
	value := "<sip:+447840844894@o2.co.uk>,<tel:+447840844894>"
	if got := firstSIPHeaderURI(value); got != "sip:+447840844894@o2.co.uk" {
		t.Fatalf("firstSIPHeaderURI = %q", got)
	}
}

func TestBuildSIPRequestResponseAcknowledgesNotify(t *testing.T) {
	request := "NOTIFY sip:user@example SIP/2.0\r\n" +
		"Via: SIP/2.0/TCP 192.0.2.1:6060;branch=z9hG4bK-notify\r\n" +
		"From: <sip:server@example>;tag=server\r\n" +
		"To: <sip:user@example>\r\n" +
		"Call-ID: notify-call\r\n" +
		"CSeq: 1 NOTIFY\r\n" +
		"Event: reg\r\nContent-Length: 0\r\n\r\n"
	response, err := buildSIPRequestResponse(request, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"SIP/2.0 200 OK", "Via: SIP/2.0/TCP 192.0.2.1:6060;branch=z9hG4bK-notify",
		"Call-ID: notify-call", "CSeq: 1 NOTIFY", "To: <sip:user@example>;tag=",
	} {
		if !strings.Contains(response, want) {
			t.Fatalf("NOTIFY response omitted %q: %q", want, response)
		}
	}
}
