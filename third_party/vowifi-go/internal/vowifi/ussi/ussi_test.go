package ussi

import (
	"strings"
	"testing"
)

func TestEncodeDecodeXML(t *testing.T) {
	p := &XMLPayload{Version: "1.0"}
	p.Session.ID = "s1"
	p.Dialog.Text = "hello"
	out, err := EncodeXML(p)
	if err != nil {
		t.Fatalf("EncodeXML: %v", err)
	}
	back, err := DecodeXML(out)
	if err != nil {
		t.Fatalf("DecodeXML: %v", err)
	}
	if back.Session.ID != "s1" {
		t.Errorf("session id = %q", back.Session.ID)
	}
}

func TestLooksLikeMenu(t *testing.T) {
	if !LooksLikeMenu("1. Balance\n2. Top-up") {
		t.Error("menu should be detected")
	}
	if LooksLikeMenu("Your balance is $10") {
		t.Error("plain message should not be a menu")
	}
}

func TestParseResult(t *testing.T) {
	r, err := ParseResult("s1", "1. Balance\n2. Top-up")
	if err != nil {
		t.Fatalf("ParseResult: %v", err)
	}
	if r.Code != "1" {
		t.Errorf("menu code = %q", r.Code)
	}
	r, _ = ParseResult("s1", "Your balance is $10")
	if r.Code != "0" {
		t.Errorf("plain code = %q", r.Code)
	}
}

func TestIsContentType(t *testing.T) {
	if !IsContentType("application/vnd.3gpp.ussd+xml") {
		t.Error("ussd+xml should be detected")
	}
	if IsContentType("application/sdp") {
		t.Error("sdp should not be detected")
	}
}

func TestBuildMultipartBody(t *testing.T) {
	body := BuildMultipartBody([]byte("v=0\r\n"), []byte("<vxml/>"))
	if !strings.Contains(string(body), "multipart/mixed") {
		t.Error("missing multipart header")
	}
	part := ExtractFromMultipart(body, "application/vnd.3gpp.ussd+xml")
	if !strings.Contains(string(part), "<vxml/>") {
		t.Errorf("extracted part = %q", part)
	}
}

func TestServiceLifecycle(t *testing.T) {
	svc := NewService()
	res, err := svc.Send("*100#")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if svc.ActiveSessionID() != res.SessionID {
		t.Errorf("active = %q, want %q", svc.ActiveSessionID(), res.SessionID)
	}
	if _, err := svc.Continue(res.SessionID, "1"); err != nil {
		t.Fatalf("Continue: %v", err)
	}
	if err := svc.Cancel(res.SessionID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if svc.ActiveSessionID() != "" {
		t.Error("active should be cleared after cancel")
	}
}

func TestBuildSDP(t *testing.T) {
	sdp := BuildSDP("10.0.0.1", "call-1")
	if !strings.Contains(string(sdp), "m=message") {
		t.Errorf("sdp = %q", sdp)
	}
}
