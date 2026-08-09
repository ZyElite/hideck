package dns

import (
	"context"
	"net"
	"reflect"
	"testing"
)

func TestRegistrarTransportCandidates(t *testing.T) {
	tests := map[string][]string{
		"udp": {"udp"}, " UDP ": {"udp"}, "tcp": {"tcp", "udp"},
		"auto": {"tcp", "udp"}, "": {"tcp", "udp"}, "tls": {"tcp", "udp"},
	}
	for input, want := range tests {
		if got := registrarTransportCandidates(input); !reflect.DeepEqual(got, want) {
			t.Fatalf("candidates(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestParseResolvConf(t *testing.T) {
	contents := []byte("# comment\nnameserver 1.1.1.1\nnameserver 2001:4860:4860::8888\n" +
		"nameserver 1.1.1.1\nnameserver invalid\n")
	servers := parseResolvConf(contents)
	if len(servers) != 2 || servers[0].String() != "1.1.1.1" || servers[1].To4() != nil {
		t.Fatalf("servers = %v", servers)
	}
}

func TestFilterDNSServersForBind(t *testing.T) {
	servers := []net.IP{nil, net.ParseIP("2001:db8::53"), net.ParseIP("192.0.2.53")}
	preferred, fallback := FilterDNSServersForBind(servers, net.ParseIP("192.0.2.10"))
	if got := ipStrings(preferred); len(got) != 1 || got[0] != "192.0.2.53" {
		t.Fatalf("preferred = %v", got)
	}
	if got := ipStrings(fallback); len(got) != 1 || got[0] != "2001:db8::53" {
		t.Fatalf("fallback = %v", got)
	}
}

func TestOrderDNSServersByPreference(t *testing.T) {
	servers := []net.IP{net.ParseIP("192.0.2.53"), net.ParseIP("2001:db8::53")}
	got := ipStrings(OrderDNSServersByPreference(servers, true))
	if len(got) != 2 || got[0] != "2001:db8::53" || got[1] != "192.0.2.53" {
		t.Fatalf("ordered = %v", got)
	}
}

func TestRegistrarDiscoveryDNSServerStages(t *testing.T) {
	assigned := []net.IP{net.ParseIP("192.0.2.53")}
	stages := registrarDiscoveryDNSServerStages(
		assigned, net.ParseIP("192.0.2.10"), true,
		[]net.IP{net.ParseIP("192.0.2.54")}, true, nil,
	)
	if len(stages) != 1 || stages[0].Source != "dns" || stages[0].Servers[0].String() != "192.0.2.53" {
		t.Fatalf("stages = %#v", stages)
	}
	assigned[0][0] ^= 0xff
	if stages[0].Servers[0].String() != "192.0.2.53" {
		t.Fatal("stage did not clone assigned DNS address")
	}
}

func TestRegistrarDiscoveryStagesUseRecoveredPublicResolver(t *testing.T) {
	stages := registrarDiscoveryDNSServerStages(nil, nil, false, nil, true, nil)
	if len(stages) != 1 || stages[0].Source != "publicdns" {
		t.Fatalf("stages = %#v", stages)
	}
	if got := stages[0].Servers[0].String(); got != "1.1.1.1" {
		t.Fatalf("public DNS = %q", got)
	}
}

func TestParserRegistrarHostPort(t *testing.T) {
	host, port, ok := parserRegistrarHostPort("sips:[2001:db8::1]:5070;transport=tcp", 5060)
	if !ok || host != "2001:db8::1" || port != 5070 {
		t.Fatalf("parsed = %q %d %v", host, port, ok)
	}
	host, port, ok = parserRegistrarHostPort("pcscf.example?x=1", 5060)
	if !ok || host != "pcscf.example" || port != 5060 {
		t.Fatalf("defaulted = %q %d %v", host, port, ok)
	}
	host, port, ok = parserRegistrarHostPort("pcscf.example:invalid", 5060)
	if !ok || host != "pcscf.example" || port != 0 {
		t.Fatalf("invalid port = %q %d %v", host, port, ok)
	}
}

func TestExpandRegistrarCandidatesPreservesUnresolvedHost(t *testing.T) {
	got := ExpandRegistrarCandidates(
		context.Background(), "sip:pcscf.invalid,sip:pcscf.invalid,[2001:db8::1]:5070,bad.example:bad",
		true, nil, nil,
	)
	want := "pcscf.invalid:5060,[2001:db8::1]:5070"
	if got != want {
		t.Fatalf("expanded = %q, want %q", got, want)
	}
}

func TestStructuredDiscoveryWithoutServersReturnsError(t *testing.T) {
	if _, err := DiscoverRegistrarCandidatesViaDNS("ims.example", nil, 0); err == nil {
		t.Fatal("expected missing DNS server error")
	}
}

func TestNormalizeRegistrarCandidatesRetainsAdditiveAPI(t *testing.T) {
	got := NormalizeRegistrarCandidates([]RegistrarCandidate{
		{Host: "a.example", Transport: "udp"},
		{Host: "a.example", Transport: "udp"},
		{Host: "b.example", Transport: "tls"},
	})
	if len(got) != 2 || got[0].Port != 5060 || got[1].Port != 5061 {
		t.Fatalf("normalized = %#v", got)
	}
}

func ipStrings(ips []net.IP) []string {
	result := make([]string, 0, len(ips))
	for _, ip := range ips {
		result = append(result, ip.String())
	}
	return result
}
