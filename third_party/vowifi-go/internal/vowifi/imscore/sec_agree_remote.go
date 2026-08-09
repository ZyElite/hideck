package imscore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

type securityIPResolver func(context.Context, string) ([]net.IP, error)

func ipAddressFamily(ip net.IP) int {
	if ip == nil {
		return 0
	}
	if ip.To4() != nil {
		return 4
	}
	if ip.To16() != nil {
		return 6
	}
	return 0
}

func normalizeIPForFamily(ip net.IP, family int) net.IP {
	if ipAddressFamily(ip) != family {
		return nil
	}
	if family == 4 {
		return append(net.IP(nil), ip.To4()...)
	}
	return append(net.IP(nil), ip.To16()...)
}

func remoteIPFromConn(conn net.Conn) net.IP {
	if conn == nil || conn.RemoteAddr() == nil {
		return nil
	}
	switch address := conn.RemoteAddr().(type) {
	case *net.TCPAddr:
		return append(net.IP(nil), address.IP...)
	case *net.UDPAddr:
		return append(net.IP(nil), address.IP...)
	}
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return nil
	}
	return net.ParseIP(strings.Trim(strings.SplitN(host, "%", 2)[0], "[]"))
}

func normalizeRemoteHostCandidate(value string) string {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "sips:") {
		value = value[len("sips:"):]
	} else if strings.HasPrefix(lower, "sip:") {
		value = value[len("sip:"):]
	}
	if at := strings.LastIndex(value, "@"); at >= 0 {
		value = value[at+1:]
	}
	if boundary := strings.IndexAny(value, ";?/"); boundary >= 0 {
		value = value[:boundary]
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		return strings.Trim(strings.SplitN(host, "%", 2)[0], "[]")
	}
	return strings.Trim(strings.SplitN(value, "%", 2)[0], "[]")
}

func selectIPSec3GPPRemoteIP(
	ctx context.Context,
	localIP net.IP,
	conn net.Conn,
	candidates []string,
	resolve securityIPResolver,
) (net.IP, error) {
	family := ipAddressFamily(localIP)
	if family == 0 {
		return nil, errors.New("ipsec3gpp: local IP 地址族不可识别")
	}
	if remote := normalizeIPForFamily(remoteIPFromConn(conn), family); remote != nil {
		return remote, nil
	}
	lastHost := ""
	for _, candidate := range candidates {
		host := normalizeRemoteHostCandidate(candidate)
		if host == "" {
			continue
		}
		lastHost = host
		if remote := normalizeIPForFamily(net.ParseIP(host), family); remote != nil {
			return remote, nil
		}
		if resolve == nil {
			continue
		}
		resolved, err := resolve(ctx, host)
		if err != nil {
			continue
		}
		for _, ip := range resolved {
			if remote := normalizeIPForFamily(ip, family); remote != nil {
				return remote, nil
			}
		}
	}
	if lastHost != "" {
		return nil, fmt.Errorf("ipsec3gpp: 无同地址族 remote IP: %s", lastHost)
	}
	return nil, errors.New("ipsec3gpp: remote IP 不可用")
}

func (s *Service) selectNegotiatedIPSecRemote(ctx context.Context, session *registerSession) (net.IP, error) {
	s.mu.RLock()
	conn := s.registrationTCP
	registrationRemote := cloneUDPAddr(s.registrationRemote)
	registrar := s.registrar
	s.mu.RUnlock()
	candidates := []string{parseRemoteIPFromPath(session.path)}
	if registrationRemote != nil && registrationRemote.IP != nil {
		candidates = append(candidates, registrationRemote.IP.String())
	}
	candidates = append(candidates, registrar, s.cfg.Registrar)
	return selectIPSec3GPPRemoteIP(ctx, s.cfg.LocalIP, conn, candidates, s.resolveSecurityIPs)
}

func (s *Service) resolveSecurityIPs(ctx context.Context, host string) ([]net.IP, error) {
	if lookup, ok := s.cfg.IMSNetwork.(interface {
		LookupIPs(context.Context, string) ([]net.IP, error)
	}); ok {
		return lookup.LookupIPs(ctx, host)
	}
	ip, err := s.cfg.IMSNetwork.ResolveIP(ctx, host)
	if err != nil {
		return nil, err
	}
	return []net.IP{ip}, nil
}
