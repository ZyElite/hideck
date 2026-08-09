package imscore

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

func normalizeSecurityMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case securityModePlain:
		return securityModePlain
	case securityModeIPSec:
		return securityModeIPSec
	default:
		return ""
	}
}

func (s *Service) recordSecurityMode(mode, reason string, fallback bool) {
	mode = normalizeSecurityMode(mode)
	s.mu.Lock()
	s.updateSecurityModeLocked(mode)
	s.securityFallbackReason = strings.TrimSpace(reason)
	s.signalingFailureReason = ""
	s.mu.Unlock()
	if fallback {
		s.securityFallbackCount.Add(1)
	}
}

func (s *Service) recordSignalingFailure(mode, reason string, err error) {
	s.mu.Lock()
	s.updateSecurityModeLocked(normalizeSecurityMode(mode))
	s.securityFallbackReason = strings.TrimSpace(reason)
	s.signalingReady = false
	if err != nil {
		s.signalingFailureReason = err.Error()
	}
	s.mu.Unlock()
}

func (s *Service) updateSecurityModeLocked(mode string) {
	if mode == "" {
		return
	}
	if s.effectiveSecurityMode != mode || s.signalingGeneration == 0 {
		s.signalingGeneration++
	}
	s.effectiveSecurityMode = mode
}

func (s *Service) effectiveSecurityModeLocked() string {
	if mode := normalizeSecurityMode(s.effectiveSecurityMode); mode != "" {
		return mode
	}
	if len(s.spiPairs) > 0 {
		return securityModeIPSec
	}
	return securityModePlain
}

func (s *Service) evaluateSignalingReadyLocked(registered bool) (bool, string) {
	if !registered {
		return false, s.signalingFailureReason
	}
	if s.transport == nil {
		return false, "missing_sipgo_client"
	}
	if s.effectiveSecurityModeLocked() == securityModeIPSec {
		if !s.externalTransport && s.registrationTCP == nil {
			return false, "missing_tcp_conn"
		}
		if len(s.spiPairs) == 0 {
			return false, "ipsec_not_installed"
		}
		if strings.TrimSpace(s.securityVerify) == "" {
			return false, "missing_security_verify"
		}
		if s.protectedClientPort < 1 || s.protectedServerPort < 1 {
			return false, "missing_ipsec_local_ports"
		}
	}
	if !s.externalTransport && s.registrationIO == nil && s.registrationTCP == nil {
		return false, "missing_remote_endpoint"
	}
	if !s.externalTransport && !s.validSignalingRemoteEndpointLocked() {
		return false, "missing_remote_endpoint"
	}
	return true, ""
}

func (s *Service) validSignalingRemoteEndpointLocked() bool {
	if s.registrationRemote != nil && validSignalingRemoteEndpoint(
		s.registrationRemote.IP.String(), s.registrationRemote.Port,
	) {
		return true
	}
	if remote := remoteIPFromConn(s.registrationTCP); remote != nil {
		return validSignalingRemoteEndpoint(remote.String(), tcpPortFromAddr(s.registrationTCP.RemoteAddr()))
	}
	registrar := normalizeRemoteHostCandidate(s.registrar)
	if registrar == "" {
		registrar = normalizeRemoteHostCandidate(s.cfg.Registrar)
	}
	return validSignalingRemoteEndpoint(registrar, defaultSIPPort)
}

func validSignalingRemoteEndpoint(host string, port int) bool {
	return strings.TrimSpace(host) != "" && port > 0 && port <= 65535
}

func tcpPortFromAddr(address net.Addr) int {
	if address == nil {
		return 0
	}
	_, portText, err := net.SplitHostPort(address.String())
	if err != nil {
		return 0
	}
	port, _ := strconv.Atoi(portText)
	return port
}

func (s *Service) computeSignalingRuntimeReadinessLocked(registered bool) {
	ready, reason := s.evaluateSignalingReadyLocked(registered)
	s.signalingReady = ready
	s.signalingFailureReason = reason
}

func (s *Service) markSignalingReady() {
	s.mu.Lock()
	mode := s.effectiveSecurityModeLocked()
	s.updateSecurityModeLocked(mode)
	s.computeSignalingRuntimeReadinessLocked(true)
	s.mu.Unlock()
}

func (s *Service) releaseUnusedProtectedReservations() {
	s.mu.Lock()
	server := s.securityServerIO
	reservation := s.clientPortReserve
	s.securityServerIO = nil
	s.clientPortReserve = nil
	s.protectedClientPort = 0
	s.protectedServerPort = 0
	s.mu.Unlock()
	closeRegistrationReservations(server, reservation)
}

func (s *Service) removeInstalledIPSec3GPP() error {
	s.mu.RLock()
	installed := len(s.spiPairs) > 0
	s.mu.RUnlock()
	if !installed {
		return nil
	}
	return s.removeIPSec3GPPPolicy()
}

func (s *Service) removeIPSec3GPPPolicy() error {
	remover, ok := s.cfg.IMSNetwork.(interface{ RemoveIPSec3GPP() error })
	if !ok {
		return errors.New("imscore: IMS network cannot remove installed 3GPP IPsec policy")
	}
	if err := remover.RemoveIPSec3GPP(); err != nil {
		return err
	}
	s.mu.Lock()
	s.spiPairs = nil
	s.securityVerify = ""
	s.mu.Unlock()
	return nil
}

func (s *Service) rollbackInstalledIPSec(cause error, previousRemote *net.UDPAddr) error {
	cleanupErr := s.removeIPSec3GPPPolicy()
	s.mu.Lock()
	s.registrationRemote = cloneUDPAddr(previousRemote)
	s.mu.Unlock()
	if cleanupErr != nil {
		return errors.Join(cause, fmt.Errorf("imscore: rollback negotiated 3GPP IPsec: %w", cleanupErr))
	}
	return cause
}

func (s *Service) setProtectedRegistrarEndpoint(remoteIP net.IP, port uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(remoteIP) == 0 {
		return &net.AddrError{Err: "missing IP", Addr: ""}
	}
	if s.registrationRemote == nil {
		s.registrationRemote = &net.UDPAddr{}
	}
	s.registrationRemote.IP = append(net.IP(nil), remoteIP...)
	s.registrationRemote.Port = int(port)
	return nil
}
