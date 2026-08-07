package imscore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

const defaultSIPPort = 5060

func (s *Service) ensureRegistrationTransport(ctx context.Context) error {
	if s.transport == nil {
		return errors.New("imscore: no SIP transport")
	}
	if s.transport.hasSendFn() {
		return nil
	}
	if s.cfg.IMSNetwork == nil {
		return errors.New("imscore: no IMS network")
	}
	remote, err := s.resolveRegistrar(ctx)
	if err != nil {
		return err
	}
	local := &net.UDPAddr{IP: s.cfg.LocalIP, Port: 0}
	conn, err := s.cfg.IMSNetwork.ListenPacket("udp", local)
	if err != nil {
		return fmt.Errorf("imscore: listen on IMS network: %w", err)
	}
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		s.cfg.LocalPort = addr.Port
	}
	serverConn, err := s.listenSecurityServerPort()
	if err != nil {
		_ = conn.Close()
		return err
	}
	s.mu.Lock()
	s.registrationIO = conn
	s.securityServerIO = serverConn
	s.registrationRemote = cloneUDPAddr(remote)
	if serverConn != nil {
		if addr, ok := serverConn.LocalAddr().(*net.UDPAddr); ok {
			s.protectedServerPort = addr.Port
		}
	}
	s.mu.Unlock()
	s.transport.SetSendFn(func(request string) error {
		remote := s.currentRegistrationRemote()
		if remote == nil {
			return errors.New("imscore: registrar address is unavailable")
		}
		if _, err := conn.WriteTo([]byte(request), remote); err != nil {
			return fmt.Errorf("imscore: send REGISTER datagram: %w", err)
		}
		return nil
	})
	s.networkDone.Add(1)
	go s.readRegistrationResponses(conn)
	if serverConn != nil {
		s.networkDone.Add(1)
		go s.readRegistrationResponses(serverConn)
	}
	return nil
}

func (s *Service) listenSecurityServerPort() (net.PacketConn, error) {
	if !s.cfg.IPSec3GPPEnabled {
		return nil, nil
	}
	address := &net.UDPAddr{IP: s.cfg.LocalIP, Port: 0}
	conn, err := s.cfg.IMSNetwork.ListenPacket("udp", address)
	if err != nil {
		return nil, fmt.Errorf("imscore: reserve protected server port: %w", err)
	}
	return conn, nil
}

func cloneUDPAddr(address *net.UDPAddr) *net.UDPAddr {
	if address == nil {
		return nil
	}
	return &net.UDPAddr{IP: append(net.IP(nil), address.IP...), Port: address.Port, Zone: address.Zone}
}

func (s *Service) currentRegistrationRemote() *net.UDPAddr {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneUDPAddr(s.registrationRemote)
}

func (s *Service) setProtectedRegistrarPort(port uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.registrationRemote == nil {
		return errors.New("imscore: registrar address is unavailable")
	}
	s.registrationRemote.Port = int(port)
	return nil
}

func (s *Service) resolveRegistrar(ctx context.Context) (*net.UDPAddr, error) {
	target := strings.TrimSpace(s.cfg.Registrar)
	if target == "" {
		target = s.discoverRegistrar(ctx)
	}
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		return nil, fmt.Errorf("imscore: parse registrar %q: %w", target, err)
	}
	ip, err := s.cfg.IMSNetwork.ResolveIP(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("imscore: resolve registrar %s: %w", host, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return nil, fmt.Errorf("imscore: parse registrar port %q: %w", portText, err)
	}
	return &net.UDPAddr{IP: ip, Port: port}, nil
}

func (s *Service) discoverRegistrar(ctx context.Context) string {
	domain := strings.TrimSpace(s.cfg.Domain)
	type srvResolver interface {
		LookupSRV(context.Context, string, string, string) (string, uint16, error)
	}
	if resolver, ok := s.cfg.IMSNetwork.(srvResolver); ok {
		host, port, err := resolver.LookupSRV(ctx, "sip", "udp", domain)
		if err == nil && strings.TrimSpace(host) != "" && port != 0 {
			return net.JoinHostPort(strings.TrimSpace(host), strconv.Itoa(int(port)))
		}
	}
	return net.JoinHostPort(domain, strconv.Itoa(defaultSIPPort))
}

func (s *Service) readRegistrationResponses(conn net.PacketConn) {
	defer s.networkDone.Done()
	buffer := make([]byte, 64*1024)
	for {
		n, _, err := conn.ReadFrom(buffer)
		if err != nil {
			return
		}
		raw := string(buffer[:n])
		response := parseSIPResponse(raw)
		if response != nil && response.StatusCode != 0 {
			s.transport.DeliverResponse(response)
		} else {
			s.transport.DeliverRequest(raw)
		}
	}
}
