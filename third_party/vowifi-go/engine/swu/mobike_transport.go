package swu

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/iniwex5/vowifi-go/engine/ipsec"
)

func (s *Session) mobikeAddressSpec(local, remote string) (mobikeAddressSpec, error) {
	if strings.TrimSpace(local) == "" || strings.TrimSpace(remote) == "" {
		return mobikeAddressSpec{}, errors.New("swu: MOBIKE could not resolve both endpoint addresses")
	}
	transport := s.transport()
	remotePort := s.cfg.EpDGPort
	if transport != nil && transport.RemotePort() > 0 && transport.RemotePort() <= int(^uint16(0)) {
		remotePort = uint16(transport.RemotePort())
	}
	if remotePort == 0 {
		remotePort = 4500
	}
	localHost, localPort, err := splitMOBIKEEndpoint(local, s.cfg.LocalPort)
	if err != nil {
		return mobikeAddressSpec{}, fmt.Errorf("swu: invalid MOBIKE local endpoint: %w", err)
	}
	remoteHost, remotePort, err := splitMOBIKEEndpoint(remote, remotePort)
	if err != nil {
		return mobikeAddressSpec{}, fmt.Errorf("swu: invalid MOBIKE remote endpoint: %w", err)
	}
	return mobikeAddressSpec{
		localHost: localHost, localPort: localPort, remoteHost: remoteHost, remotePort: remotePort,
	}, nil
}

func splitMOBIKEEndpoint(value string, defaultPort uint16) (string, uint16, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", 0, errors.New("empty endpoint")
	}
	if ip := net.ParseIP(value); ip != nil {
		return ip.String(), defaultPort, nil
	}
	host, rawPort, err := net.SplitHostPort(value)
	if err != nil {
		if strings.Contains(value, ":") {
			return "", 0, err
		}
		return value, defaultPort, nil
	}
	port, err := strconv.ParseUint(rawPort, 10, 16)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port %q: %w", rawPort, err)
	}
	return host, uint16(port), nil
}

func (s *Session) migrateMOBIKETransport(spec mobikeAddressSpec) error {
	replacement, err := s.newMOBIKETransport(spec)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			replacement.Stop()
		}
	}()
	previous := s.transport()
	if previous == nil {
		return errors.New("swu: active transport disappeared during MOBIKE")
	}
	control, err := s.pauseIKEControlForMobility()
	if err != nil {
		return err
	}
	if err := s.updateKernelMOBIKETransport(replacement); err != nil {
		return errors.Join(err, s.resumeIKEControlAfterMobility(control))
	}
	s.setTransport(replacement)
	s.rebindUserspaceTransport(replacement)
	if err := s.resumeIKEControlAfterMobility(control); err != nil {
		s.setTransport(previous)
		s.rebindUserspaceTransport(previous)
		kernelErr := s.updateKernelMOBIKETransport(previous)
		controlErr := s.resumeIKEControlAfterMobility(control)
		return errors.Join(fmt.Errorf("swu: start migrated IKE control plane: %w", err), kernelErr, controlErr)
	}
	s.commitMOBIKEAddresses(spec, replacement)
	s.startNetEventMonitor()
	previous.Stop()
	committed = true
	return nil
}

func (s *Session) newMOBIKETransport(spec mobikeAddressSpec) (ipsec.Transport, error) {
	local := net.JoinHostPort(spec.localHost, strconv.Itoa(int(spec.localPort)))
	remote := net.JoinHostPort(spec.remoteHost, strconv.Itoa(int(spec.remotePort)))
	transport, err := s.createMOBIKETransport(local, remote)
	if err != nil {
		return nil, err
	}
	if transport == nil {
		return nil, errors.New("swu: MOBIKE transport factory returned nil")
	}
	if err := transport.Start(); err != nil {
		transport.Stop()
		return nil, fmt.Errorf("swu: start MOBIKE transport: %w", err)
	}
	return transport, nil
}

func (s *Session) createMOBIKETransport(local, remote string) (ipsec.Transport, error) {
	if s.cfg.TransportFactory != nil {
		transport, err := s.cfg.TransportFactory(local, remote)
		if err != nil {
			return nil, fmt.Errorf("swu: create injected MOBIKE transport: %w", err)
		}
		return transport, nil
	}
	if strings.TrimSpace(configuredProxyAddress(s.cfg)) == "" {
		transport, err := ipsec.NewSocketManager(s.cfg.DeviceID, local, remote, s.cfg.DNSServer)
		if err != nil {
			return nil, fmt.Errorf("swu: create MOBIKE socket: %w", err)
		}
		return transport, nil
	}
	proxy := ipsec.Socks5Config{}
	if s.cfg.Proxy != nil {
		proxy = *s.cfg.Proxy
	}
	proxy.ProxyAddr, proxy.RemoteAddr = configuredProxyAddress(s.cfg), remote
	proxy.Username, proxy.Password = configuredProxyCredentials(s.cfg)
	proxy.DNSServer, proxy.DeviceID = s.cfg.DNSServer, s.cfg.DeviceID
	transport, err := ipsec.NewSocks5Transport(proxy)
	if err != nil {
		return nil, fmt.Errorf("swu: create MOBIKE SOCKS5 transport: %w", err)
	}
	return transport, nil
}

func (s *Session) commitMOBIKEAddresses(spec mobikeAddressSpec, transport ipsec.Transport) {
	s.mu.Lock()
	s.cfg.LocalAddr = spec.localHost
	s.cfg.LocalIP = append(net.IP(nil), transport.LocalIP()...)
	s.cfg.EPDGAddr, s.cfg.EpDGAddr = spec.remoteHost, spec.remoteHost
	s.cfg.EpDGPort = spec.remotePort
	s.remoteIP = append(net.IP(nil), transport.RemoteIP()...)
	s.remotePort = transport.RemotePort()
	s.mu.Unlock()
}
