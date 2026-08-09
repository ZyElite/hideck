package imscore

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

func (s *Service) handleFatalTransactionError(err error) {
	if s == nil || !isFatalSIPTransportError(err) {
		return
	}
	s.markSignalingDead(fmt.Errorf("imscore: fatal SIP transport error: %w", err))
}

func isFatalSIPTransportError(err error) bool {
	return IsFatalNetworkError(err)
}

func (s *Service) markSignalingDead(err error) {
	if s == nil || err == nil {
		return
	}
	packet, stream, marked := s.detachDeadSignaling(err)
	if !marked {
		return
	}
	closeDeadSignaling(packet, stream)
	s.transport.terminateClientTransactions(transactionTransportError(err))
	s.transitionRegStatus(registrationRejectedTemporary)
	s.notifySMSReadiness()
	s.reportRegistrationRuntimeError(err)
}

func (s *Service) detachDeadSignaling(err error) (net.PacketConn, net.Conn, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.signalingReady && strings.TrimSpace(s.signalingFailureReason) != "" {
		return nil, nil, false
	}
	packet := s.registrationIO
	stream := s.registrationTCP
	s.registrationIO = nil
	s.registrationTCP = nil
	s.registrationTCPProtected = false
	s.registrationTransport = ""
	s.signalingGeneration++
	s.signalingReady = false
	s.signalingFailureReason = err.Error()
	s.regState = regFailed
	s.transport.SetSendFn(func(string) error {
		return errors.New("imscore: registered SIP transport is not connected")
	})
	return packet, stream, true
}

func closeDeadSignaling(packet net.PacketConn, stream net.Conn) {
	if packet != nil {
		_ = packet.Close()
	}
	if stream != nil {
		_ = stream.Close()
	}
}
