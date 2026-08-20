package imscore

import "testing"

func TestContainsHeaderToken(t *testing.T) {
	if !containsHeaderToken("path, sec-agree, outbound", "outbound") {
		t.Fatal("outbound token was not detected")
	}
	if containsHeaderToken("path, outbound-flow", "outbound") {
		t.Fatal("partial outbound token was accepted")
	}
}

func TestContainsSIPParameter(t *testing.T) {
	path := `<sip:edge.example;lr;ob>, <sip:pcscf.example;lr>`
	if !containsSIPParameter(path, "ob") {
		t.Fatal("Path ob parameter was not detected")
	}
	contact := `<sip:user@example>;reg-id=1;+sip.instance="<urn:uuid:test>"`
	if !containsSIPParameter(contact, "reg-id") {
		t.Fatal("Contact reg-id parameter was not detected")
	}
	if containsSIPParameter(contact, "id") {
		t.Fatal("partial Contact parameter was accepted")
	}
}
