package imscore

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
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
	if s.startProtectedRegistrationRecovery(nil, err) {
		return
	}
	packet, stream, marked := s.detachDeadSignaling(err, nil)
	if !marked {
		return
	}
	s.finishSignalingFailure(packet, stream, err)
}

func (s *Service) markRegistrationPacketSignalingDead(expected net.PacketConn, err error) bool {
	if s == nil || expected == nil || err == nil {
		return false
	}
	packet, stream, marked := s.detachDeadSignaling(err, expected)
	if !marked {
		return false
	}
	s.finishSignalingFailure(packet, stream, err)
	return true
}

func (s *Service) finishSignalingFailure(packet net.PacketConn, stream net.Conn, err error) {
	closeDeadSignaling(packet, stream)
	s.transport.terminateClientTransactions(transactionTransportError(err))
	s.transitionRegStatus(registrationRejectedTemporary)
	s.notifySMSReadiness()
	s.reportRegistrationRuntimeError(err)
}

func (s *Service) detachDeadSignaling(err error, expectedPacket net.PacketConn) (net.PacketConn, net.Conn, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedPacket != nil && s.registrationIO != expectedPacket {
		return nil, nil, false
	}
	if !s.signalingReady && strings.TrimSpace(s.signalingFailureReason) != "" {
		return nil, nil, false
	}
	packet := s.registrationIO
	stream := s.registrationTCP
	s.registrationIO = nil
	s.registrationTCP = nil
	s.registrationTCPProtected = false
	s.registrationTransport = ""
	s.registrationRefreshAt = time.Time{}
	s.subscriptionRefreshAt = time.Time{}
	s.nextRegister = time.Time{}
	s.signalingGeneration++
	s.signalingReady = false
	s.signalingFailureReason = err.Error()
	s.regState = regFailed
	s.lastPingOK.Store(false)
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
