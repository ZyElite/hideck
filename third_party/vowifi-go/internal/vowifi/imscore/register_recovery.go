package imscore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const registrationTransportRecoveryTimeout = 30 * time.Second

func (s *Service) recoverableProtectedRegistrationLocked(conn net.Conn) bool {
	if s.registrationTCP == nil || (conn != nil && s.registrationTCP != conn) ||
		!s.registrationTCPProtected || s.regSession == nil {
		return false
	}
	security := s.regSession.security
	return !s.externalTransport && security != nil && security.server != nil
}

// startProtectedRegistrationRecovery claims a protected stream failure before
// pending SIP transactions observe it. This prevents the same TCP reset from
// also tearing down the still-valid SWu/IPsec session.
func (s *Service) startProtectedRegistrationRecovery(expected net.Conn, cause error) bool {
	if s == nil || s.stopped() {
		return false
	}
	if s.registrationRecoveryBusy.Load() {
		return true
	}
	conn, claimed := s.claimProtectedRegistrationRecovery(expected)
	if !claimed {
		return s.registrationRecoveryBusy.Load()
	}
	_ = conn.Close()
	s.transport.terminateClientTransactions(transactionTransportError(cause))
	s.transitionRegStatus(registrationRejectedTemporary)
	s.notifySMSReadiness()
	s.networkDone.Add(1)
	go s.recoverProtectedRegistration(cause)
	return true
}

func (s *Service) claimProtectedRegistrationRecovery(expected net.Conn) (net.Conn, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.recoverableProtectedRegistrationLocked(expected) ||
		!s.registrationRecoveryBusy.CompareAndSwap(false, true) {
		return nil, false
	}
	conn := s.registrationTCP
	s.registrationTCP = nil
	s.registrationTCPProtected = false
	s.regState = regFailed
	s.transport.SetSendFn(func(string) error {
		return errors.New("imscore: registered SIP transport is not connected")
	})
	return conn, true
}

func (s *Service) recoverProtectedRegistration(cause error) {
	defer s.networkDone.Done()
	stopCtx, stopCancel := registrationContextUntilStop(s.stop)
	defer stopCancel()
	ctx, cancel := context.WithTimeout(stopCtx, registrationTransportRecoveryTimeout)
	defer cancel()

	logging.Info("IMS protected REGISTER transport recovery starting",
		"device", s.DeviceID(), "cause", cause)
	for {
		err := s.Register(ctx)
		if err != nil {
			s.registrationRecoveryBusy.Store(false)
			if s.stopped() || errors.Is(err, context.Canceled) {
				return
			}
			s.reportRegistrationRuntimeError(fmt.Errorf(
				"imscore: protected registration transport recovery failed after %v: %w", cause, err))
			return
		}
		if !s.protectedRegistrationConnected() {
			cause = errors.New("imscore: recovered registration transport closed before it became stable")
			continue
		}
		s.registrationRecoveryBusy.Store(false)
		if s.protectedRegistrationConnected() {
			logging.Info("IMS protected REGISTER transport recovered", "device", s.DeviceID())
			return
		}
		completionErr := errors.New("imscore: recovered registration transport closed during recovery completion")
		if s.startProtectedRegistrationRecovery(nil, completionErr) {
			return
		}
		s.reportRegistrationRuntimeError(completionErr)
		return
	}
}

func (s *Service) protectedRegistrationConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.regState == regRegistered && s.registrationTCP != nil && s.registrationTCPProtected
}
