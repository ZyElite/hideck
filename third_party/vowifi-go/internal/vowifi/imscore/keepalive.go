package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const (
	imsKeepaliveInterval           = 60 * time.Second
	imsKeepaliveTransactionTimeout = 5 * time.Second
	imsKeepaliveFailureLimit       = 3
)

func (s *Service) startIMSKeepalive() {
	if s == nil {
		return
	}
	s.keepaliveOnce.Do(func() {
		s.UpdateLastPingAt(time.Now())
		s.networkDone.Add(1)
		go s.runIMSKeepalive()
	})
}

func (s *Service) runIMSKeepalive() {
	defer s.networkDone.Done()
	interval := s.keepaliveInterval
	if interval <= 0 {
		interval = imsKeepaliveInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	failures := 0
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			failures = s.handleIMSKeepaliveTick(failures)
		}
	}
}

func (s *Service) handleIMSKeepaliveTick(failures int) int {
	if !s.IsRegistered() {
		return failures
	}
	err := s.sendIMSKeepalive()
	if err == nil {
		s.keepaliveSuccessOnce.Do(func() {
			logging.Info("IMS SIP keepalive established", "device", s.DeviceID())
		})
		return 0
	}

	failures++
	logging.WarnRate("ims-keepalive", "IMS SIP keepalive failed",
		"device", s.DeviceID(), "attempt", failures, "err", err)
	if failures < s.keepaliveFailureLimit {
		return failures
	}
	return s.reconnectAfterKeepaliveFailure(err)
}

func (s *Service) reconnectAfterKeepaliveFailure(keepaliveErr error) int {
	if err := s.TriggerRegisterImmediate(); err != nil {
		s.reportRegistrationRuntimeError(fmt.Errorf(
			"imscore: keepalive failed and registration recovery failed: %w",
			errors.Join(keepaliveErr, err),
		))
		return s.keepaliveFailureLimit
	}
	return 0
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
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	response, err := s.transport.RoundTrip(ctx, request)
	if err != nil {
		return fmt.Errorf("OPTIONS transaction: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("OPTIONS rejected with status %d (%s)", response.StatusCode, response.Reason)
	}
	s.UpdateLastPingAt(time.Now())
	return nil
}

func (s *Service) buildIMSKeepaliveOPTIONS() (string, error) {
	profile, err := s.reserveRegisteredSIPProfile()
	if err != nil {
		return "", fmt.Errorf("imscore: keepalive registered profile: %w", err)
	}
	branch := "z9hG4bK" + newBranch()
	var request strings.Builder
	fmt.Fprintf(&request, "OPTIONS %s SIP/2.0\r\n", profile.LocalURI)
	fmt.Fprintf(&request, "Via: SIP/2.0/%s %s;rport;branch=%s\r\n",
		transportUpper(profile.Transport), profile.LocalAddress, branch)
	if profile.ServiceRoute != "" {
		request.WriteString("Route: " + profile.ServiceRoute + "\r\n")
	}
	fmt.Fprintf(&request, "From: <%s>;tag=%s\r\n", profile.LocalURI, newTag())
	fmt.Fprintf(&request, "To: <%s>\r\n", profile.LocalURI)
	fmt.Fprintf(&request, "Call-ID: %s\r\nCSeq: %d OPTIONS\r\n", newCallID(), profile.InitialCSeq)
	request.WriteString("Contact: " + profile.ContactHeader + "\r\n")
	request.WriteString("Max-Forwards: 70\r\n")
	request.WriteString("P-Preferred-Identity: <" + profile.LocalURI + ">\r\n")
	if profile.SecurityVerify != "" {
		request.WriteString("Security-Verify: " + profile.SecurityVerify + "\r\n")
	}
	if profile.PANI != "" {
		request.WriteString("P-Access-Network-Info: " + profile.PANI + "\r\n")
	}
	if profile.UserAgent != "" {
		request.WriteString("User-Agent: " + profile.UserAgent + "\r\n")
	}
	request.WriteString("Content-Length: 0\r\n\r\n")
	return request.String(), nil
}
