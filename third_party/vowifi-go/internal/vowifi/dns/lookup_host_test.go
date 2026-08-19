package dns

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"
)

func TestLookupHostIPStagedRejectsInvalidConfiguredServer(t *testing.T) {
	ClearHostIPCache()
	t.Cleanup(ClearHostIPCache)
	if _, err := LookupHostIPStaged(context.Background(), "epdg.test", "not a DNS address"); err == nil {
		t.Fatal("invalid custom DNS address was accepted")
	}
}

func TestLookupHostIPStagedAcceptsConfiguredHostnameEndpoint(t *testing.T) {
	ClearHostIPCache()
	t.Cleanup(ClearHostIPCache)
	got, err := lookupHostIPStaged(context.Background(), "epdg.test", "dns.google:53", hostLookupFuncs{
		viaServer: func(_ context.Context, _ string, server string) ([]net.IP, error) {
			if server != "dns.google:53" {
				t.Fatalf("server = %q", server)
			}
			return []net.IP{net.IPv4(192, 0, 2, 41)}, nil
		},
		viaSystem: func(context.Context, string) ([]net.IP, error) {
			t.Fatal("system resolver should not run after configured success")
			return nil, errors.New("unused")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ipStrings(got), []string{"192.0.2.41"}) {
		t.Fatalf("ips = %v", ipStrings(got))
	}
}

func TestLookupHostIPStagedPrefersLiveLookupOverCache(t *testing.T) {
	ClearHostIPCache()
	t.Cleanup(ClearHostIPCache)
	storeHostIPs("epdg-live.test", []net.IP{net.IPv4(192, 0, 2, 31)})
	got, err := lookupHostIPStaged(context.Background(), "epdg-live.test", "9.9.9.9", hostLookupFuncs{
		viaServer: func(context.Context, string, string) ([]net.IP, error) {
			return []net.IP{net.IPv4(192, 0, 2, 32)}, nil
		},
		viaSystem: func(context.Context, string) ([]net.IP, error) {
			t.Fatal("system resolver should not run after configured success")
			return nil, errors.New("unused")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ipStrings(got), []string{"192.0.2.32"}) {
		t.Fatalf("ips = %v", ipStrings(got))
	}
}

func TestLookupHostIPStagedUsesCacheOnlyAfterTimeout(t *testing.T) {
	ClearHostIPCache()
	t.Cleanup(ClearHostIPCache)
	storeHostIPs("epdg-fallback.test", []net.IP{net.IPv4(192, 0, 2, 31)})
	got, err := lookupHostIPStaged(context.Background(), "epdg-fallback.test", "9.9.9.9", hostLookupFuncs{
		viaServer: func(context.Context, string, string) ([]net.IP, error) {
			return nil, context.DeadlineExceeded
		},
		viaSystem: func(context.Context, string) ([]net.IP, error) {
			return nil, errors.New("nxdomain")
		},
		public: []string{"1.1.1.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ipStrings(got), []string{"192.0.2.31"}) {
		t.Fatalf("cached ips = %v", ipStrings(got))
	}
}

func TestLookupHostIPStagedDoesNotUseCacheOnDefinitiveFailure(t *testing.T) {
	ClearHostIPCache()
	t.Cleanup(ClearHostIPCache)
	storeHostIPs("epdg-nx.test", []net.IP{net.IPv4(192, 0, 2, 31)})
	_, err := lookupHostIPStaged(context.Background(), "epdg-nx.test", "9.9.9.9", hostLookupFuncs{
		viaServer: func(context.Context, string, string) ([]net.IP, error) {
			return nil, errors.New("nxdomain")
		},
		viaSystem: func(context.Context, string) ([]net.IP, error) {
			return nil, errors.New("nxdomain")
		},
		public: []string{"1.1.1.1"},
	})
	if err == nil {
		t.Fatal("expected live lookup failure without cache fallback")
	}
}

func TestLookupHostIPStagedFallsBackToSystemThenPublic(t *testing.T) {
	ClearHostIPCache()
	t.Cleanup(ClearHostIPCache)
	var servers []string
	got, err := lookupHostIPStaged(context.Background(), "epdg-stages.test", "9.9.9.9", hostLookupFuncs{
		viaServer: func(_ context.Context, _ string, server string) ([]net.IP, error) {
			servers = append(servers, server)
			if server == "8.8.8.8" {
				return []net.IP{net.IPv4(192, 0, 2, 40)}, nil
			}
			return nil, errors.New("refused")
		},
		viaSystem: func(context.Context, string) ([]net.IP, error) {
			servers = append(servers, "system")
			return nil, errors.New("nxdomain")
		},
		public: []string{"1.1.1.1", "8.8.8.8"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(servers, []string{"9.9.9.9", "system", "1.1.1.1", "8.8.8.8"}) {
		t.Fatalf("servers = %#v", servers)
	}
	if !reflect.DeepEqual(ipStrings(got), []string{"192.0.2.40"}) {
		t.Fatalf("ips = %v", ipStrings(got))
	}
}
