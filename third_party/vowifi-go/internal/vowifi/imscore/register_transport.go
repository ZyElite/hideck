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
	if reconnect, client, server := s.protectedReconnectParameters(); reconnect {
		return s.dialProtectedRegistrationTCP(ctx, client, server)
	}
	if s.transport.hasSendFn() {
		candidates, err := registerTransportCandidates(s.cfg.Transport)
		if err != nil {
			return err
		}
		s.mu.Lock()
		if s.registrationIO == nil && s.registrationTCP == nil {
			s.externalTransport = true
			s.registrationTransport = candidates[0]
		}
		s.mu.Unlock()
		return nil
	}
	if s.cfg.IMSNetwork == nil {
		return errors.New("imscore: no IMS network")
	}
	serverListener, clientReservation, err := s.reserveProtectedTCPPorts()
	if err != nil {
		return err
	}
	if err := s.openInitialRegistrationTransport(ctx, serverListener, clientReservation); err != nil {
		closeRegistrationReservations(serverListener, clientReservation)
		return err
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
	return s.dialProtectedRegistrationTCP(ctx, client, server)
}

func (s *Service) dialProtectedRegistrationTCP(ctx context.Context, client, server securityMechanism) error {
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
	logging.Info("IMS protected REGISTER TCP connected",
		"local_port", local.Port, "remote_port", remote.Port)
	s.activateProtectedRegistrationTCP(conn)
	return nil
}

func (s *Service) protectedReconnectParameters() (bool, securityMechanism, securityMechanism) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.externalTransport || (s.registrationTCP != nil && s.registrationTCPProtected) || s.regSession == nil || s.regSession.security == nil || s.regSession.security.server == nil {
		return false, securityMechanism{}, securityMechanism{}
	}
	return true, s.regSession.security.client, *s.regSession.security.server
}

func (s *Service) protectedTransportState() (external, connected bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.externalTransport, s.registrationTCP != nil && s.registrationTCPProtected
}

func (s *Service) activateProtectedRegistrationTCP(conn net.Conn) {
	s.mu.Lock()
	previous := s.registrationTCP
	s.registrationTCP = conn
	if previous != nil && previous != conn {
		s.registrationPreviousTCP = previous
	}
	s.registrationTCPProtected = true
	s.registrationTransport = "tcp"
	s.mu.Unlock()
	s.transport.SetSendFn(func(request string) error {
		return s.writeSIPStream(conn, request)
	})
	s.networkDone.Add(1)
	go s.readRegistrationStream(conn)
}

func (s *Service) finalizeRegistrationTransportSwitch() {
	s.mu.Lock()
	previous := s.registrationPreviousTCP
	s.registrationPreviousTCP = nil
	s.mu.Unlock()
	if previous != nil {
		_ = previous.Close()
	}
}

func (s *Service) readRegistrationResponses(conn net.PacketConn) {
	defer s.networkDone.Done()
	s.receiverStarted()
	defer s.receiverStopped()
	buffer := make([]byte, 64*1024)
	for {
		n, remote, err := conn.ReadFrom(buffer)
		if err != nil {
			return
		}
		err = s.dispatchInboundSIP(string(buffer[:n]), func(response string) error {
			if _, writeErr := conn.WriteTo([]byte(response), remote); writeErr != nil {
				return fmt.Errorf("imscore: write SIP datagram: %w", writeErr)
			}
			return nil
		})
		if err != nil {
			logging.WarnRate("ims-udp-inbound", "IMS UDP inbound handling failed", "err", err)
		}
	}
}

func (s *Service) acceptProtectedSIP(listener net.Listener) {
	defer s.networkDone.Done()
	s.receiverStarted()
	defer s.receiverStopped()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		if !s.trackProtectedConnection(conn) {
			_ = conn.Close()
			return
		}
		s.networkDone.Add(1)
		go s.serveProtectedSIPConnection(conn)
	}
}

func (s *Service) serveProtectedSIPConnection(conn net.Conn) {
	defer s.networkDone.Done()
	defer s.untrackProtectedConnection(conn)
	defer conn.Close()
	s.readRegistrationStreamSync(conn)
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
	defer s.clearClosedRegistrationTCP(conn)
	s.receiverStarted()
	defer s.receiverStopped()
	s.readRegistrationStreamSync(conn)
}

func (s *Service) clearClosedRegistrationTCP(conn net.Conn) {
	_ = conn.Close()
	s.mu.Lock()
	if s.registrationTCP == conn {
		s.registrationTCP = nil
		s.registrationTCPProtected = false
	}
	s.mu.Unlock()
}

func (s *Service) readRegistrationStreamSync(conn net.Conn) {
	reader := bufio.NewReader(conn)
	for {
		raw, err := readSIPStreamMessage(reader)
		if err != nil {
			return
		}
		if err := s.dispatchInboundSIP(raw, func(response string) error {
			return s.writeSIPStream(conn, response)
		}); err != nil {
			logging.WarnRate("ims-tcp-inbound", "IMS TCP inbound handling failed", "err", err)
		}
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
