package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsheaders"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/sipkit"
)

const (
	imsKeepaliveInterval           = 60 * time.Second
	imsKeepaliveTransactionTimeout = 5 * time.Second
	imsKeepaliveFailureLimit       = 3
	imsMaintenancePollInterval     = 5 * time.Second
	imsMaintenanceMinimumDelay     = 100 * time.Millisecond
	imsRegistrationRefreshAdvance  = 60 * time.Second
	imsKeepaliveFlow               = "options_keepalive"
)

type imsMaintenanceAction uint8

const (
	imsMaintenanceIdle imsMaintenanceAction = iota
	imsMaintenanceRefresh
	imsMaintenanceKeepalive
)

func (s *Service) startIMSKeepalive() {
	if s == nil {
		return
	}
	s.keepaliveOnce.Do(func() {
		s.UpdateLastPingAt(time.Now())
		s.networkDone.Add(1)
		go s.runIMSMaintenance()
	})
	s.signalIMSMaintenance()
}

func (s *Service) runIMSMaintenance() {
	defer s.networkDone.Done()
	for {
		if !s.waitForIMSMaintenance(s.computeNextIMSWakeTime(time.Now())) {
			return
		}
		switch s.nextIMSMaintenanceAction(time.Now()) {
		case imsMaintenanceRefresh:
			s.refreshRegistration()
		case imsMaintenanceKeepalive:
			s.handleIMSKeepaliveTick()
		}
	}
}

func (s *Service) waitForIMSMaintenance(wakeAt time.Time) bool {
	delay := time.Until(wakeAt)
	if delay < imsMaintenanceMinimumDelay {
		delay = imsMaintenanceMinimumDelay
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-s.stop:
		return false
	case <-s.maintenanceWake:
		return true
	case <-timer.C:
		return true
	}
}

func (s *Service) computeNextIMSWakeTime(now time.Time) time.Time {
	s.mu.RLock()
	registered := s.regState == regRegistered
	refreshAt := s.registrationRefreshAt
	lastTrafficAt := s.lastPingAt
	interval := s.keepaliveInterval
	s.mu.RUnlock()

	next := now.Add(imsMaintenancePollInterval)
	if !registered {
		return next
	}
	if !refreshAt.IsZero() && refreshAt.Before(next) {
		next = refreshAt
	}
	keepaliveAt := lastTrafficAt.Add(interval)
	if lastTrafficAt.IsZero() {
		keepaliveAt = now
	}
	if keepaliveAt.Before(next) {
		next = keepaliveAt
	}
	return next
}

func (s *Service) nextIMSMaintenanceAction(now time.Time) imsMaintenanceAction {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.regState != regRegistered {
		return imsMaintenanceIdle
	}
	if s.registrationRefreshAt.IsZero() || !now.Before(s.registrationRefreshAt) {
		return imsMaintenanceRefresh
	}
	if s.lastPingAt.IsZero() || !now.Before(s.lastPingAt.Add(s.keepaliveInterval)) {
		return imsMaintenanceKeepalive
	}
	return imsMaintenanceIdle
}

func (s *Service) handleIMSKeepaliveTick() {
	if !s.IsRegistered() {
		return
	}
	s.recordIMSKeepaliveResult(s.sendIMSKeepalive(), time.Now())
}

func (s *Service) recordIMSKeepaliveResult(err error, completedAt time.Time) {
	s.mu.Lock()
	s.lastPingAt = completedAt
	if err == nil {
		s.keepaliveFailures = 0
	} else {
		s.keepaliveFailures++
	}
	failures := s.keepaliveFailures
	limit := s.keepaliveFailureLimit
	s.mu.Unlock()
	if err == nil {
		s.keepaliveSuccessOnce.Do(func() {
			logging.Info("IMS SIP keepalive established", "device", s.DeviceID())
		})
		return
	}
	logging.WarnRate("ims-keepalive", "IMS SIP keepalive failed",
		"device", s.DeviceID(), "attempt", failures, "err", err)
	if failures >= limit {
		s.requestRuntimeReconnect(err)
	}
}

func (s *Service) requestRuntimeReconnect(keepaliveErr error) {
	s.mu.Lock()
	if s.regState != regRegistered {
		s.mu.Unlock()
		return
	}
	s.regState = regFailed
	s.registrationRefreshAt = time.Time{}
	s.keepaliveFailures = 0
	s.mu.Unlock()
	s.transitionRegStatus(registrationRejectedTemporary)
	s.notifySMSReadiness()
	err := fmt.Errorf("imscore: fast reconnect requested after %d keepalive failures: %w",
		s.keepaliveFailureLimit, keepaliveErr)
	logging.WarnRate("ims-fast-reconnect", "IMS SIP keepalive requested runtime rebuild",
		"device", s.DeviceID(), "err", err)
	s.reportRegistrationRuntimeError(err)
}

func (s *Service) signalIMSMaintenance() {
	select {
	case s.maintenanceWake <- struct{}{}:
	default:
	}
}

func registrationRefreshDelay(expires time.Duration) time.Duration {
	if expires > imsRegistrationRefreshAdvance {
		return expires - imsRegistrationRefreshAdvance
	}
	if expires > 0 && expires/2 > 0 {
		return expires / 2
	}
	return time.Second
}

func (s *Service) sendIMSKeepalive() error {
	request, err := s.buildIMSKeepaliveOPTIONS()
	if err != nil {
		return err
	}
	timeout := s.keepaliveTimeout
	if timeout <= 0 {
		timeout = imsKeepaliveTransactionTimeout
	}
	response, _, err := s.dispatchOutboundRequest(
		context.Background(), imsKeepaliveFlow, request, timeout, true,
	)
	if err != nil {
		return fmt.Errorf("OPTIONS transaction: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("OPTIONS rejected with status %d (%s)", response.StatusCode, response.Reason)
	}
	s.UpdateLastPingAt(time.Now())
	return nil
}

func (s *Service) buildIMSKeepaliveOPTIONS() (*sip.Request, error) {
	profile, err := s.reserveRegisteredSIPProfile()
	if err != nil {
		return nil, fmt.Errorf("imscore: keepalive registered profile: %w", err)
	}
	aor, err := parseKeepaliveURI(profile.LocalURI)
	if err != nil {
		return nil, err
	}
	contact, err := parseKeepaliveContact(profile.ContactHeader)
	if err != nil {
		return nil, err
	}
	securityMode := "disabled"
	if strings.TrimSpace(profile.SecurityVerify) != "" {
		securityMode = securityModeIPSec
	}
	return sipkit.BuildIMSRequest(sip.OPTIONS, aor, sipkit.IMSRequestOptions{
		Transport: profile.Transport, Branch: "z9hG4bK" + newBranch(),
		FromURI: aor, FromTag: newTag(), ToURI: aor,
		CallID: newCallID(), CSeq: uint32(profile.InitialCSeq), Contact: contact,
		Kind: sipkit.RequestKindOutOfDialog, SecurityMode: securityMode,
		AddRPort:          true,
		AddUserAgent:      strings.TrimSpace(profile.UserAgent) != "",
		PreferredIdentity: imsheaders.PreferredIdentityHeaderValue(profile.LocalURI),
		Runtime: sipkit.IMSRuntimeSnapshot{
			ServiceRoute: profile.ServiceRoute, SecVerify: profile.SecurityVerify,
			PAccessNetworkInfo: profile.PANI, UserAgent: profile.UserAgent,
			LocalAddr: profile.LocalAddress, Transport: profile.Transport,
		},
	})
}

func parseKeepaliveURI(value string) (sip.Uri, error) {
	var uri sip.Uri
	if err := sip.ParseUri(strings.TrimSpace(value), &uri); err != nil {
		return sip.Uri{}, fmt.Errorf("imscore: keepalive AOR: %w", err)
	}
	return uri, nil
}

func parseKeepaliveContact(value string) (*sip.ContactHeader, error) {
	parser := sip.HeadersParser(sip.DefaultHeadersParser())
	headers, err := parser.ParseHeader(nil, []byte("Contact: "+value))
	if err != nil {
		return nil, fmt.Errorf("imscore: keepalive Contact: %w", err)
	}
	if len(headers) != 1 {
		return nil, errors.New("imscore: keepalive Contact did not contain one address")
	}
	contact, ok := headers[0].(*sip.ContactHeader)
	if !ok {
		return nil, errors.New("imscore: keepalive Contact has an unexpected header type")
	}
	return contact, nil
}
