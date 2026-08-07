package imscore

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost/identity"
)

func TestSIPStatusText(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{180, "Ringing"},
		{200, "OK"},
		{401, "Unauthorized"},
		{486, "Busy Here"},
		{503, "Service Unavailable"},
	}
	for _, c := range cases {
		if got := SIPStatusText(c.code); got != c.want {
			t.Errorf("SIPStatusText(%d) = %q, want %q", c.code, got, c.want)
		}
	}
}

func TestCountryISO2FromMCC(t *testing.T) {
	if got := CountryISO2FromMCC("310"); got != "US" {
		t.Errorf("310 -> %q, want US", got)
	}
	if got := CountryISO2FromMCC("460"); got != "CN" {
		t.Errorf("460 -> %q, want CN", got)
	}
}

func TestIsFatalNetworkError(t *testing.T) {
	fatal := []error{
		errors.New("connection refused"),
		&net.OpError{Op: "dial", Err: errors.New("network is unreachable")},
	}
	for _, e := range fatal {
		if !IsFatalNetworkError(e) {
			t.Errorf("%v should be fatal", e)
		}
	}
	if IsFatalNetworkError(errors.New("i/o timeout")) {
		t.Error("timeout should not be fatal")
	}
	if IsFatalNetworkError(nil) {
		t.Error("nil should not be fatal")
	}
}

func TestGenerateStablePAccessNetworkInfo(t *testing.T) {
	pani := GenerateStablePAccessNetworkInfo("user@example.com")
	if pani != `IEEE-802.11; i-wlan-node-id="b6c9a289323b"` {
		t.Errorf("pani = %q", pani)
	}
	withCountry := AppendPAccessNetworkCountry(pani, "US")
	if withCountry != pani+";country=US" {
		t.Errorf("appended = %q", withCountry)
	}
	if got := AppendPAccessNetworkCountry(withCountry, "GB"); got != withCountry {
		t.Errorf("duplicate country = %q", got)
	}
}

func TestGenerateStableWlanNodeID(t *testing.T) {
	id := GenerateStableWlanNodeID("user@example.com")
	if id != "b6c9a289323b" {
		t.Errorf("node id = %q", id)
	}
	if GenerateStableWlanNodeID("user@example.com") != id {
		t.Error("node id should be stable")
	}
	if GenerateStableWlanNodeID("   ") != "" {
		t.Error("blank seed should not create a node id")
	}
}

func TestGenerateStablePAccessNetworkInfoByIdentity(t *testing.T) {
	ident := identity.IMSIdentity{
		IMPI:   "310260123456789@ims.example",
		IMPU:   "sip:310260123456789@ims.example",
		Domain: "ims.example",
	}
	want := `IEEE-802.11; i-wlan-node-id="ba25793d37ec"`
	if got := GenerateStablePAccessNetworkInfoByIdentity(ident); got != want {
		t.Fatalf("identity PANI = %q, want %q", got, want)
	}
}

func TestBuildIMSConfigFromCarrier(t *testing.T) {
	ident := testIdentity()
	cfg := BuildIMSConfigFromCarrier("dev-1", ident, "epdg.example.com")
	if cfg.IMPI != "310260123456789@ims.mnc026.mcc310.3gppnetwork.org" {
		t.Errorf("impi = %q", cfg.IMPI)
	}
	if cfg.Domain != "ims.mnc026.mcc310.3gppnetwork.org" {
		t.Errorf("domain = %q", cfg.Domain)
	}
	if cfg.EPDGAddr != "epdg.example.com" {
		t.Errorf("epdg = %q", cfg.EPDGAddr)
	}
	// ApplyResolvedIMSIdentityToConfig overwrites.
	cfg.IMPI = "old"
	ApplyResolvedIMSIdentityToConfig(cfg, ident)
	if cfg.IMPI != "310260123456789@ims.mnc026.mcc310.3gppnetwork.org" {
		t.Errorf("impi after apply = %q", cfg.IMPI)
	}
}

func TestServiceStartAndSnapshot(t *testing.T) {
	cfg := &IMSConfig{
		DeviceID:    "dev-1",
		IMSI:        "310260123456789",
		IMPI:        "310260123456789@ims.example.com",
		Domain:      "ims.example.com",
		AKAProvider: stubAKAProvider{},
	}
	svc, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	svc.Transport().SetSendFn(func(string) error { return nil })
	svc.mu.Lock()
	svc.regState = regRegistered
	svc.mu.Unlock()
	if svc.IPSec3GPPEnabled() {
		t.Error("IPsec should be disabled by default")
	}
	svc.SetEnableIPSec3GPP(true)
	if !svc.IPSec3GPPEnabled() {
		t.Error("IPsec should be enabled after toggle")
	}
	st := svc.Status()
	m := st.ToMap()
	if m["registered"] != true {
		t.Errorf("ToMap = %+v", m)
	}
	snap := svc.Snapshot()
	if snap["reg_state"] != "registered" {
		t.Errorf("snapshot = %+v", snap)
	}
	if svc.Session() != "idle" {
		t.Errorf("session = %q", svc.Session())
	}
}

func registerResponseForRequest(request string, status int, headers map[string]string) *sipResponse {
	responseHeaders := make(map[string]string, len(headers)+1)
	for name, value := range headers {
		responseHeaders[name] = value
	}
	responseHeaders["Via"] = sipHeaderValue(request, "Via")
	return &sipResponse{
		StatusCode: status,
		CallID:     sipHeaderValue(request, "Call-ID"),
		CSeq:       sipHeaderValue(request, "CSeq"),
		Headers:    responseHeaders,
	}
}

func TestServiceMethods(t *testing.T) {
	cfg := &IMSConfig{
		DeviceID:    "dev-1",
		IMSI:        "310260123456789",
		IMPI:        "310260123456789@ims.example.com",
		Domain:      "ims.example.com",
		AKAProvider: stubAKAProvider{},
	}
	svc, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	svc.Transport().SetSendFn(func(request string) error {
		svc.transport.DeliverResponse(registerResponseForRequest(request, 200, nil))
		return nil
	})
	if err := svc.Subscribe("reg.example.com"); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	h := &imscoreDialogHandle{callID: "call-1", fromTag: "f", toTag: "t"}
	if err := svc.SendDialogRequest(h, "BYE", ""); err != nil {
		t.Fatalf("SendDialogRequest: %v", err)
	}
	if err := svc.SendReliableProvisionalPRACK(h); err != nil {
		t.Fatalf("PRACK: %v", err)
	}
	inv := &imscoreServerInviteHandle{callID: "call-in"}
	if err := svc.RejectServerInvite(inv); err != nil {
		t.Fatalf("RejectServerInvite: %v", err)
	}
	if err := svc.TriggerFastReconnect(); err != nil {
		t.Fatalf("TriggerFastReconnect: %v", err)
	}
	svc.UpdateLastPingAt(time.Now())
}

// testIdentity returns a carrier identity.
func testIdentity() identity.IMSIdentity {
	return identity.IMSIdentity{
		IMPI:   "310260123456789@ims.mnc026.mcc310.3gppnetwork.org",
		IMPU:   "sip:310260123456789@ims.mnc026.mcc310.3gppnetwork.org",
		Domain: "ims.mnc026.mcc310.3gppnetwork.org",
	}
}
