package imscore

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const defaultSIPPort = 5060

func (s *Service) ensureRegistrationTransport(ctx context.Context) error {
	if s.transport == nil {
		return errors.New("imscore: no SIP transport")
	}
	if s.transport.hasSendFn() {
		s.mu.Lock()
		if s.registrationIO == nil && s.registrationTCP == nil {
			s.externalTransport = true
		}
		s.mu.Unlock()
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
	serverListener, clientReservation, err := s.reserveProtectedTCPPorts()
	if err != nil {
		_ = conn.Close()
		return err
	}
	s.mu.Lock()
	s.registrationIO = conn
	s.securityServerIO = serverListener
	s.clientPortReserve = clientReservation
	s.registrationRemote = cloneUDPAddr(remote)
	if serverListener != nil {
		s.protectedServerPort = tcpPort(serverListener.Addr())
	}
	if clientReservation != nil {
		s.protectedClientPort = tcpPort(clientReservation.Addr())
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
	if serverListener != nil {
		s.networkDone.Add(1)
		go s.acceptProtectedSIP(serverListener)
	}
	return nil
}

func (s *Service) reserveProtectedTCPPorts() (net.Listener, net.Listener, error) {
	if !s.cfg.IPSec3GPPEnabled {
		return nil, nil, nil
	}
	server, err := s.cfg.IMSNetwork.ListenTCP(&net.TCPAddr{IP: s.cfg.LocalIP})
	if err != nil {
		return nil, nil, fmt.Errorf("imscore: reserve protected server port: %w", err)
	}
	client, err := s.cfg.IMSNetwork.ListenTCP(&net.TCPAddr{IP: s.cfg.LocalIP})
	if err != nil {
		_ = server.Close()
		return nil, nil, fmt.Errorf("imscore: reserve protected client port: %w", err)
	}
	return server, client, nil
}

func tcpPort(address net.Addr) int {
	if tcpAddress, ok := address.(*net.TCPAddr); ok {
		return tcpAddress.Port
	}
	return 0
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

func (s *Service) connectProtectedRegistrationTCP(ctx context.Context, client, server securityMechanism) error {
	s.mu.Lock()
	reservation := s.clientPortReserve
	s.clientPortReserve = nil
	s.mu.Unlock()
	if reservation == nil {
		return errors.New("imscore: protected client port was not reserved")
	}
	if err := reservation.Close(); err != nil {
		return fmt.Errorf("imscore: release protected client port: %w", err)
	}
	registrationRemote := s.currentRegistrationRemote()
	if registrationRemote == nil || registrationRemote.IP == nil {
		return errors.New("imscore: registrar IP unavailable for protected TCP")
	}
	local := &net.TCPAddr{IP: s.cfg.LocalIP, Port: int(client.PortC)}
	remote := &net.TCPAddr{IP: registrationRemote.IP, Port: int(server.PortS)}
	conn, err := s.cfg.IMSNetwork.DialTCPContext(ctx, local, remote)
	if err != nil {
		return fmt.Errorf("imscore: connect protected REGISTER TCP: %w", err)
	}
	s.activateProtectedRegistrationTCP(conn)
	return nil
}

func (s *Service) activateProtectedRegistrationTCP(conn net.Conn) {
	s.mu.Lock()
	s.registrationTCP = conn
	s.mu.Unlock()
	s.transport.SetSendFn(func(request string) error {
		if _, err := io.Copy(conn, strings.NewReader(request)); err != nil {
			return fmt.Errorf("imscore: send protected REGISTER stream: %w", err)
		}
		return nil
	})
	s.networkDone.Add(1)
	go s.readRegistrationStream(conn)
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
		s.deliverSIPMessage(string(buffer[:n]))
	}
}

func (s *Service) acceptProtectedSIP(listener net.Listener) {
	defer s.networkDone.Done()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		if !s.trackProtectedConnection(conn) {
			_ = conn.Close()
			return
		}
		s.readRegistrationStreamSync(conn)
		s.untrackProtectedConnection(conn)
		_ = conn.Close()
	}
}

func (s *Service) trackProtectedConnection(conn net.Conn) bool {
	s.protectedConnMu.Lock()
	defer s.protectedConnMu.Unlock()
	select {
	case <-s.stop:
		return false
	default:
		s.protectedConns[conn] = struct{}{}
		return true
	}
}

func (s *Service) untrackProtectedConnection(conn net.Conn) {
	s.protectedConnMu.Lock()
	delete(s.protectedConns, conn)
	s.protectedConnMu.Unlock()
}

func (s *Service) readRegistrationStream(conn net.Conn) {
	defer s.networkDone.Done()
	s.readRegistrationStreamSync(conn)
}

func (s *Service) readRegistrationStreamSync(conn net.Conn) {
	reader := bufio.NewReader(conn)
	for {
		raw, err := readSIPStreamMessage(reader)
		if err != nil {
			return
		}
		if err := s.respondToInboundNotification(conn, raw); err != nil {
			logging.WarnRate("ims-notify-response", "IMS NOTIFY response failed", "err", err)
		}
		s.deliverSIPMessage(raw)
	}
}

func readSIPStreamMessage(reader *bufio.Reader) (string, error) {
	var message strings.Builder
	contentLength := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		message.WriteString(line)
		if name, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			contentLength, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil || contentLength < 0 {
				return "", errors.New("imscore: invalid SIP Content-Length")
			}
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	if contentLength == 0 {
		return message.String(), nil
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, body); err != nil {
		return "", err
	}
	message.Write(body)
	return message.String(), nil
}

func (s *Service) deliverSIPMessage(raw string) {
	response := parseSIPResponse(raw)
	if response != nil && response.StatusCode != 0 {
		s.transport.DeliverResponse(response)
		return
	}
	s.transport.DeliverRequest(raw)
}
