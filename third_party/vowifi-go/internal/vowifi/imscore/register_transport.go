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
	s.registrationIO = conn
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		s.cfg.LocalPort = addr.Port
	}
	s.transport.SetSendFn(func(request string) error {
		if _, err := conn.WriteTo([]byte(request), remote); err != nil {
			return fmt.Errorf("imscore: send REGISTER datagram: %w", err)
		}
		return nil
	})
	s.networkDone.Add(1)
	go s.readRegistrationResponses(conn)
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
		response := parseSIPResponse(string(buffer[:n]))
		if response != nil && response.StatusCode != 0 {
			s.transport.DeliverResponse(response)
		}
	}
}
