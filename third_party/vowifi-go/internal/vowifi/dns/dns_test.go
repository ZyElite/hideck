package dns

import (
	"testing"
)

func TestParserRegistrarHostPort(t *testing.T) {
	host, port, err := parserRegistrarHostPort("epdg.example.com:4500")
	if err != nil || host != "epdg.example.com" || port != 4500 {
		t.Errorf("parse = %q %d %v", host, port, err)
	}
	host, port, err = parserRegistrarHostPort("epdg.example.com")
	if err != nil || host != "epdg.example.com" || port != 5060 {
		t.Errorf("parse no port = %q %d %v", host, port, err)
	}
}

func TestExpandRegistrarCandidates(t *testing.T) {
	cands := []RegistrarCandidate{
		{Host: "a.example.com", Transport: "udp"},
		{Host: "a.example.com", Transport: "udp"}, // duplicate
		{Host: "b.example.com", Transport: "tls"},
	}
	got := ExpandRegistrarCandidates(cands)
	if len(got) != 2 {
		t.Fatalf("expanded = %d, want 2", len(got))
	}
	if got[0].Port != 5060 {
		t.Errorf("udp default port = %d", got[0].Port)
	}
	if got[1].Port != 5061 {
		t.Errorf("tls default port = %d", got[1].Port)
	}
}

func TestOrderDNSServersByPreference(t *testing.T) {
	servers := []string{"8.8.8.8:53", "127.0.0.1:53", "192.168.1.1:53"}
	got := OrderDNSServersByPreference(servers)
	if got[0] != "127.0.0.1:53" {
		t.Errorf("loopback should be first: %v", got)
	}
}

func TestFilterDNSServersForBind(t *testing.T) {
	got := FilterDNSServersForBind([]string{"8.8.8.8:53", "", "not-an-ip", "1.1.1.1:53"})
	if len(got) != 2 {
		t.Errorf("filtered = %v", got)
	}
}

func TestTransportOf(t *testing.T) {
	if got := transportOf("_sip._udp.example.com"); got != "udp" {
		t.Errorf("transport = %q", got)
	}
}
