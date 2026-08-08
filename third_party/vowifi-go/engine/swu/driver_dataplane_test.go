package swu

import (
	"context"
	"net"
	"reflect"
	"testing"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func TestNormalizeDataplaneMode(t *testing.T) {
	tests := map[string]string{
		"": DataplaneModeUserspace, " USERSPACE ": DataplaneModeUserspace,
		"tun": DataplaneModeTUN, "XFRMI": DataplaneModeXFRMI,
	}
	for input, expected := range tests {
		actual, err := normalizeDataplaneMode(input)
		if err != nil || actual != expected {
			t.Fatalf("normalizeDataplaneMode(%q) = %q, %v", input, actual, err)
		}
	}
	if _, err := normalizeDataplaneMode("mock"); err == nil {
		t.Fatal("unsupported data plane mode was accepted")
	}
}

func TestUnsupportedDataplaneModeFailsBeforeOpeningTransport(t *testing.T) {
	session := NewSession(&Config{DataplaneMode: "mock", EPDGAddr: "127.0.0.1"})
	if err := session.Connect(context.Background()); err == nil {
		t.Fatal("unsupported data plane mode reached connection setup")
	}
	if session.socket != nil {
		t.Fatal("unsupported data plane mode opened a transport")
	}
}

func TestIPRangePrefixesProducesMinimalIPv4Cover(t *testing.T) {
	prefixes, err := ipRangePrefixes(net.ParseIP("192.0.2.5"), net.ParseIP("192.0.2.10"), false)
	if err != nil {
		t.Fatalf("ipRangePrefixes: %v", err)
	}
	actual := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		actual = append(actual, prefix.String())
	}
	expected := []string{"192.0.2.5/32", "192.0.2.6/31", "192.0.2.8/31", "192.0.2.10/32"}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("prefixes = %v, want %v", actual, expected)
	}
}

func TestIPRangePrefixesCoversWholeFamiliesWithoutEnumeration(t *testing.T) {
	ipv4, err := ipRangePrefixes(net.IPv4zero, net.IPv4bcast, false)
	if err != nil || len(ipv4) != 1 || ipv4[0].String() != "0.0.0.0/0" {
		t.Fatalf("IPv4 full range = %v, %v", ipv4, err)
	}
	ipv6, err := ipRangePrefixes(net.IPv6zero, net.ParseIP("ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"), true)
	if err != nil || len(ipv6) != 1 || ipv6[0].String() != "::/0" {
		t.Fatalf("IPv6 full range = %v, %v", ipv6, err)
	}
}

func TestDataPlaneRoutesIncludesPCSCFAndNegotiatedSelectorsOnce(t *testing.T) {
	pcscf := net.ParseIP("192.0.2.8")
	session := NewSession(&Config{})
	session.pcscfServers = []net.IP{pcscf, pcscf}
	session.childTSr = &ikev2.EncryptedPayloadTS{TrafficSelectors: []*ikev2.TrafficSelector{
		ikev2.NewTrafficSelectorIPV4Range(pcscf, pcscf, 0, 0, 65535),
		ikev2.NewTrafficSelectorIPV6Range(net.ParseIP("2001:db8::"), net.ParseIP("2001:db8::ffff"), 0, 0, 65535),
	}}
	routes, hasIPv6, err := session.dataPlaneRoutes()
	if err != nil {
		t.Fatalf("dataPlaneRoutes: %v", err)
	}
	expected := []dataPlaneRoute{{cidr: "192.0.2.8/32"}, {cidr: "2001:db8::/112", ipv6: true}}
	if !hasIPv6 || !reflect.DeepEqual(routes, expected) {
		t.Fatalf("routes = %#v, hasIPv6=%v", routes, hasIPv6)
	}
}
