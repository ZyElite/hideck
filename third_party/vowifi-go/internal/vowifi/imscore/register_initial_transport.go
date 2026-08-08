package imscore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

type initialRegistrationTransport struct {
	kind   string
	remote *net.UDPAddr
	packet net.PacketConn
	stream net.Conn
	port   int
}

func registerTransportCandidates(configured string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(configured)) {
	case "", "auto":
		return []string{"tcp", "udp"}, nil
	case "tcp":
		return []string{"tcp"}, nil
	case "udp":
		return []string{"udp"}, nil
	default:
		return nil, fmt.Errorf("imscore: unsupported REGISTER transport %q", configured)
	}
}

func (s *Service) openInitialRegistrationTransport(
	ctx context.Context,
	serverListener, clientReservation net.Listener,
) error {
	candidates, err := registerTransportCandidates(s.cfg.Transport)
	if err != nil {
		return err
	}
	if s.cfg.IPSec3GPPEnabled && isAutoRegisterTransport(s.cfg.Transport) {
		candidates = []string{"udp", "tcp"}
	}
	var failures []error
	for _, candidate := range candidates {
		opened, openErr := s.openRegisterCandidate(ctx, candidate)
		if openErr != nil {
			failures = append(failures, openErr)
			continue
		}
		s.activateInitialRegistrationTransport(opened, serverListener, clientReservation)
		return nil
	}
	return fmt.Errorf("imscore: open REGISTER transport: %w", errors.Join(failures...))
}

func isAutoRegisterTransport(configured string) bool {
	value := strings.ToLower(strings.TrimSpace(configured))
	return value == "" || value == "auto"
}

func (s *Service) openRegisterCandidate(ctx context.Context, transport string) (*initialRegistrationTransport, error) {
	remote, err := s.resolveRegistrar(ctx, transport)
	if err != nil {
		return nil, fmt.Errorf("%s registrar: %w", transport, err)
	}
	if transport == "tcp" {
		local := &net.TCPAddr{IP: s.cfg.LocalIP}
		conn, dialErr := s.cfg.IMSNetwork.DialTCPContext(ctx, local, udpToTCPAddr(remote))
		if dialErr != nil {
			return nil, fmt.Errorf("tcp connect: %w", dialErr)
		}
		return &initialRegistrationTransport{
			kind: transport, remote: remote, stream: conn, port: tcpPort(conn.LocalAddr()),
		}, nil
	}
	conn, err := s.cfg.IMSNetwork.ListenPacket("udp", &net.UDPAddr{IP: s.cfg.LocalIP})
	if err != nil {
		return nil, fmt.Errorf("udp listen: %w", err)
	}
	port := 0
	if address, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		port = address.Port
	}
	return &initialRegistrationTransport{kind: transport, remote: remote, packet: conn, port: port}, nil
}

func udpToTCPAddr(address *net.UDPAddr) *net.TCPAddr {
	return &net.TCPAddr{IP: append(net.IP(nil), address.IP...), Port: address.Port, Zone: address.Zone}
}

func (s *Service) activateInitialRegistrationTransport(
	opened *initialRegistrationTransport,
	serverListener, clientReservation net.Listener,
) {
	s.cfg.LocalPort = opened.port
	s.mu.Lock()
	s.registrationIO = opened.packet
	s.registrationTCP = opened.stream
	s.registrationTCPProtected = false
	s.registrationTransport = opened.kind
	s.securityServerIO = serverListener
	s.clientPortReserve = clientReservation
	s.registrationRemote = cloneUDPAddr(opened.remote)
	if serverListener != nil {
		s.protectedServerPort = tcpPort(serverListener.Addr())
	}
	if clientReservation != nil {
		s.protectedClientPort = tcpPort(clientReservation.Addr())
	}
	s.mu.Unlock()
	s.activateInitialSendAndReceive(opened)
	if serverListener != nil {
		s.networkDone.Add(1)
		go s.acceptProtectedSIP(serverListener)
	}
}

func (s *Service) activateInitialSendAndReceive(opened *initialRegistrationTransport) {
	if opened.stream != nil {
		configureTCPKeepalive(opened.stream)
		s.transport.SetSendFn(func(request string) error {
			return s.writeSIPStream(opened.stream, request)
		})
		s.networkDone.Add(1)
		go s.readRegistrationStream(opened.stream)
		return
	}
	s.transport.SetSendFn(func(request string) error {
		remote := s.currentRegistrationRemote()
		if remote == nil {
			return errors.New("imscore: registrar address is unavailable")
		}
		if _, err := opened.packet.WriteTo([]byte(request), remote); err != nil {
			return fmt.Errorf("imscore: send REGISTER datagram: %w", err)
		}
		return nil
	})
	s.networkDone.Add(1)
	go s.readRegistrationResponses(opened.packet)
}

func closeRegistrationReservations(serverListener, clientReservation net.Listener) {
	if serverListener != nil {
		_ = serverListener.Close()
	}
	if clientReservation != nil {
		_ = clientReservation.Close()
	}
}

func (s *Service) resolveRegistrar(ctx context.Context, transport string) (*net.UDPAddr, error) {
	target := strings.TrimSpace(s.cfg.Registrar)
	if target == "" {
		target = s.discoverRegistrar(ctx, transport)
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

func (s *Service) discoverRegistrar(ctx context.Context, transport string) string {
	domain := strings.TrimSpace(s.cfg.Domain)
	type srvResolver interface {
		LookupSRV(context.Context, string, string, string) (string, uint16, error)
	}
	if resolver, ok := s.cfg.IMSNetwork.(srvResolver); ok {
		host, port, err := resolver.LookupSRV(ctx, "sip", transport, domain)
		if err == nil && strings.TrimSpace(host) != "" && port != 0 {
			return net.JoinHostPort(strings.TrimSpace(host), strconv.Itoa(int(port)))
		}
	}
	return net.JoinHostPort(domain, strconv.Itoa(defaultSIPPort))
}
