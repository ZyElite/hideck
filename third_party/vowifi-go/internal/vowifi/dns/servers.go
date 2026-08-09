package dns

import (
	"net"
	"os"
	"strings"
	"time"
)

const (
	defaultDNSPort          = "53"
	registrarQueryTimeout   = 3 * time.Second
	defaultRegistrarSIPPort = 5060
)

type registrarDiscoveryDNSServerStage struct {
	Source  string
	Servers []net.IP
}

// ReadSystemDNSServers reads unique nameserver addresses from resolv.conf.
func ReadSystemDNSServers() []net.IP {
	contents, err := os.ReadFile("/etc/resolv.conf")
	if err != nil || len(contents) == 0 {
		return nil
	}
	return parseResolvConf(contents)
}

func parseResolvConf(contents []byte) []net.IP {
	servers := make([]net.IP, 0, 4)
	seen := make(map[string]struct{})
	for _, line := range strings.Split(string(contents), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || strings.HasPrefix(strings.TrimSpace(line), "#") || fields[0] != "nameserver" {
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(fields[1]))
		if ip == nil {
			continue
		}
		key := ip.String()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		servers = append(servers, ip)
	}
	return servers
}

// FilterDNSServersForBind separates servers matching bindIP's address family.
func FilterDNSServersForBind(servers []net.IP, bindIP net.IP) ([]net.IP, []net.IP) {
	if len(servers) == 0 {
		return nil, nil
	}
	if bindIP == nil {
		return servers, nil
	}
	preferred := make([]net.IP, 0, len(servers))
	fallback := make([]net.IP, 0, len(servers))
	wantIPv4 := bindIP.To4() != nil
	for _, server := range servers {
		if server == nil {
			continue
		}
		if (server.To4() != nil) == wantIPv4 {
			preferred = append(preferred, server)
		} else {
			fallback = append(fallback, server)
		}
	}
	return preferred, fallback
}

func defaultRegistrarPublicDNSServers() []net.IP {
	return []net.IP{net.ParseIP("1.1.1.1")}
}

func registrarDiscoveryDNSServerStages(
	assigned []net.IP,
	bindIP net.IP,
	includeSystem bool,
	system []net.IP,
	includePublic bool,
	public []net.IP,
) []registrarDiscoveryDNSServerStage {
	if len(assigned) > 0 {
		preferred, _ := FilterDNSServersForBind(assigned, bindIP)
		if len(preferred) == 0 {
			return nil
		}
		return []registrarDiscoveryDNSServerStage{{Source: "dns", Servers: cloneIPs(preferred)}}
	}
	stages := make([]registrarDiscoveryDNSServerStage, 0, 2)
	stages = appendDNSStage(stages, "systemdns", includeSystem, system, bindIP)
	if includePublic && len(public) == 0 {
		public = defaultRegistrarPublicDNSServers()
	}
	return appendDNSStage(stages, "publicdns", includePublic, public, bindIP)
}

func appendDNSStage(
	stages []registrarDiscoveryDNSServerStage,
	source string,
	include bool,
	servers []net.IP,
	bindIP net.IP,
) []registrarDiscoveryDNSServerStage {
	if !include {
		return stages
	}
	preferred, _ := FilterDNSServersForBind(servers, bindIP)
	if len(preferred) == 0 {
		return stages
	}
	return append(stages, registrarDiscoveryDNSServerStage{Source: source, Servers: cloneIPs(preferred)})
}

func cloneIPs(ips []net.IP) []net.IP {
	cloned := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if ip != nil {
			cloned = append(cloned, append(net.IP(nil), ip...))
		}
	}
	return cloned
}
