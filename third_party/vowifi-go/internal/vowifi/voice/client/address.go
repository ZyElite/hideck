package client

import (
	"fmt"
	"net"
	"strings"
)

const defaultListenAddress = ":5060"

var ignoredInterfaceFragments = [...]string{"rmnet", "tun", "tap", "docker"}

// LocalIP returns the adapter's advertised address, then falls back to the
// first non-loopback IPv4 address on a non-tunnel interface.
func (b *Bridge) LocalIP() string {
	if b != nil && b.adapter != nil {
		if externalIP := b.adapter.GetExternalIP(); externalIP != "" {
			return externalIP
		}
	}
	if ip := preferredInterfaceIPv4(); ip != "" {
		return ip
	}
	return firstGlobalIPv4()
}

// ListenHostPort resolves the adapter listen address. Wildcard IPv4 and an
// empty host are replaced with LocalIP, matching the original client contact
// construction behavior.
func (b *Bridge) ListenHostPort() (string, int) {
	listenAddress := defaultListenAddress
	if b != nil && b.adapter != nil {
		if configured := strings.TrimSpace(b.adapter.GetListenAddr()); configured != "" {
			listenAddress = configured
		}
	}
	host, portText, err := net.SplitHostPort(listenAddress)
	if err != nil {
		return "", 0
	}
	port := 5060
	_, _ = fmt.Sscanf(portText, "%d", &port)
	if host == "" || host == "0.0.0.0" {
		host = b.LocalIP()
	}
	return host, port
}

func preferredInterfaceIPv4() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, networkInterface := range interfaces {
		if ignoredInterface(networkInterface.Name) {
			continue
		}
		addresses, err := networkInterface.Addrs()
		if err == nil {
			if ip := firstIPv4(addresses); ip != "" {
				return ip
			}
		}
	}
	return ""
}

func ignoredInterface(name string) bool {
	name = strings.ToLower(name)
	for _, fragment := range ignoredInterfaceFragments {
		if strings.Contains(name, fragment) {
			return true
		}
	}
	return false
}

func firstGlobalIPv4() string {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	return firstIPv4(addresses)
}

func firstIPv4(addresses []net.Addr) string {
	for _, address := range addresses {
		ipNetwork, ok := address.(*net.IPNet)
		if !ok || ipNetwork.IP.IsLoopback() {
			continue
		}
		if ip := ipNetwork.IP.To4(); ip != nil {
			return ip.String()
		}
	}
	return ""
}
