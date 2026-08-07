package imscore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imsheaders"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const registrationSubscriptionTimeout = 10 * time.Second

type subscriptionTransaction struct {
	callID string
	cseq   int
	branch string
}

func (s *Service) startRegistrationSubscription() {
	if !s.hasProtectedRegistrationTransport() {
		return
	}
	s.networkDone.Add(1)
	go func() {
		defer s.networkDone.Done()
		ctx, cancel := context.WithTimeout(context.Background(), registrationSubscriptionTimeout)
		defer cancel()
		if err := s.sendSubscribeReg(ctx); err != nil && !s.stopped() {
			s.reportRegistrationRuntimeError(fmt.Errorf("imscore: registration event subscription failed: %w", err))
		}
	}()
}

func (s *Service) hasProtectedRegistrationTransport() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.registrationTCP != nil && s.regSession != nil &&
		s.regSession.security != nil && s.regSession.security.verifyHeader != ""
}

func (s *Service) stopped() bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

func (s *Service) reportRegistrationRuntimeError(err error) {
	select {
	case s.registerErrors <- err:
	default:
	}
}

func (s *Service) sendSubscribeReg(ctx context.Context) error {
	s.subscribeMu.Lock()
	defer s.subscribeMu.Unlock()
	transaction, request, err := s.registrationSubscriptionRequest()
	if err != nil {
		return err
	}
	logging.RunDebug("IMS SUBSCRIBE(reg) outbound", "sip", logging.RedactSIPRaw(request))
	if err := s.sendSIP(request); err != nil {
		return fmt.Errorf("send SUBSCRIBE: %w", err)
	}
	response, err := s.receiveSubscriptionResponse(ctx, transaction)
	if err != nil {
		return fmt.Errorf("receive SUBSCRIBE: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("SUBSCRIBE rejected with status %d (%s)", response.StatusCode, response.Reason)
	}
	logging.Info("IMS SUBSCRIBE(reg) 成功", "call_id", transaction.callID, "code", response.StatusCode)
	return nil
}

func (s *Service) registrationSubscriptionRequest() (subscriptionTransaction, string, error) {
	s.mu.RLock()
	session := s.regSession
	clientPort, serverPort := s.protectedClientPort, s.protectedServerPort
	s.mu.RUnlock()
	if session == nil || session.security == nil || session.security.verifyHeader == "" {
		return subscriptionTransaction{}, "", errors.New("protected registration session is unavailable")
	}
	if clientPort <= 0 || serverPort <= 0 {
		return subscriptionTransaction{}, "", errors.New("protected IMS ports are unavailable")
	}
	transaction := subscriptionTransaction{
		callID: randomHex(20),
		cseq:   session.cseq + 2,
		branch: "z9hG4bK" + randomHex(40),
	}
	return transaction, s.buildRegistrationSubscription(session, transaction, clientPort, serverPort), nil
}

func (s *Service) buildRegistrationSubscription(
	session *registerSession,
	transaction subscriptionTransaction,
	clientPort, serverPort int,
) string {
	publicID := session.publicID
	if publicID == "" {
		publicID = primaryPublicIdentity(s.cfg)
	}
	localClient := net.JoinHostPort(s.cfg.LocalIP.String(), fmt.Sprint(clientPort))
	localServer := net.JoinHostPort(s.cfg.LocalIP.String(), fmt.Sprint(serverPort))
	instance := imsheaders.NormalizeSipInstance(s.cfg.IMEI)

	var request strings.Builder
	fmt.Fprintf(&request, "SUBSCRIBE %s SIP/2.0\r\n", publicID)
	fmt.Fprintf(&request, "Via: SIP/2.0/TCP %s;rport;branch=%s\r\n", localClient, transaction.branch)
	if session.serviceRoute != "" {
		request.WriteString("Route: " + session.serviceRoute + "\r\n")
	}
	fmt.Fprintf(&request, "From: <%s>;tag=%s\r\n", publicID, randomHex(10))
	fmt.Fprintf(&request, "To: <%s>\r\n", publicID)
	fmt.Fprintf(&request, "Call-ID: %s\r\n", transaction.callID)
	fmt.Fprintf(&request, "CSeq: %d SUBSCRIBE\r\n", transaction.cseq)
	request.WriteString("Max-Forwards: 70\r\n")
	fmt.Fprintf(&request, "Contact: <sip:%s@%s>;+sip.instance=\"%s\"\r\n", session.contactUser, localServer, instance)
	request.WriteString("Require: sec-agree\r\n")
	request.WriteString("Proxy-Require: sec-agree\r\n")
	request.WriteString("P-Access-Network-Info: " + s.GetPAccessNetworkInfo() + "\r\n")
	request.WriteString("P-Preferred-Identity: <" + publicID + ">\r\n")
	request.WriteString("Security-Verify: " + session.security.verifyHeader + "\r\n")
	if userAgent := strings.TrimSpace(s.cfg.UserAgent); userAgent != "" {
		request.WriteString("User-Agent: " + userAgent + "\r\n")
	}
	fmt.Fprintf(&request, "Expires: %d\r\n", int(registerExpires(s.cfg).Seconds()))
	request.WriteString("Event: reg\r\n")
	request.WriteString("Accept: application/reginfo+xml\r\n")
	request.WriteString("Content-Length: 0\r\n\r\n")
	return request.String()
}

func (s *Service) receiveSubscriptionResponse(
	ctx context.Context,
	transaction subscriptionTransaction,
) (*sipResponse, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.stop:
			return nil, errors.New("service stopped")
		case response := <-s.transport.Responses():
			if subscriptionResponseMatches(response, transaction) && response.StatusCode >= 200 {
				return response, nil
			}
		}
	}
}

func subscriptionResponseMatches(response *sipResponse, transaction subscriptionTransaction) bool {
	if response == nil || response.CallID != transaction.callID {
		return false
	}
	cseq, method, err := parseSIPCSeq(response.CSeq)
	if err != nil || cseq != transaction.cseq || !strings.EqualFold(method, "SUBSCRIBE") {
		return false
	}
	branch, err := parseTopViaBranch(response.Header("Via"))
	return err == nil && branch == transaction.branch
}

func firstSIPHeaderURI(value string) string {
	value = strings.TrimSpace(value)
	if start := strings.IndexByte(value, '<'); start >= 0 {
		if end := strings.IndexByte(value[start+1:], '>'); end >= 0 {
			return strings.TrimSpace(value[start+1 : start+1+end])
		}
	}
	value, _, _ = strings.Cut(value, ",")
	return strings.Trim(strings.TrimSpace(value), "<>")
}
