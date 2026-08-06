package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
)

// SMSSendOptions carries optional SMS delivery parameters.
type SMSSendOptions struct {
	SuppressSendTGSuccess bool
	Encoding              string
}

// SMSSendOutcome is the result of an SMS send.
type SMSSendOutcome struct {
	Ref        string
	Err        error
	MessageID  string
	PartsTotal int
	State      string
}

// SendSMSWithResult sends an SMS and returns the outcome.
func (s *Service) SendSMSWithResult(ctx context.Context, to, text string) (*SMSSendOutcome, error) {
	return s.SendSMSWithOptions(ctx, to, text, SMSSendOptions{})
}

// SendSMSWithOptions sends an SMS with options.
func (s *Service) SendSMSWithOptions(ctx context.Context, to, text string, opts SMSSendOptions) (*SMSSendOutcome, error) {
	if s == nil || s.cfg == nil {
		return nil, errors.New("imscore: service not configured")
	}
	if !s.IsRegistered() {
		return nil, errors.New("imscore: not registered")
	}
	ref := newMessageID()
	outcome := &SMSSendOutcome{Ref: ref, MessageID: ref, PartsTotal: 1, State: "accepted"}

	// Build the SIP MESSAGE request.
	req := s.buildSMSRequest(to, text, ref)
	if err := s.sendSIP(req); err != nil {
		return nil, fmt.Errorf("imscore: send SMS: %w", err)
	}

	// Record the delivery in the store.
	if s.delivery != nil {
		imsi := s.cfg.IMSI
		if err := s.delivery.CreateSMSDelivery(ref, imsi, s.cfg.DeviceID, to, text, 1, time.Now()); err != nil {
			return nil, err
		}
	}
	s.emitSMS(outcome)
	return outcome, nil
}

// buildSMSRequest builds a SIP MESSAGE request for an SMS.
func (s *Service) buildSMSRequest(to, text, ref string) string {
	cfg := s.cfg
	callID := newCallID()
	var b strings.Builder
	b.WriteString(fmt.Sprintf("MESSAGE sip:%s@%s SIP/2.0\r\n", sanitizePhone(to), cfg.Domain))
	b.WriteString(fmt.Sprintf("Via: SIP/2.0/%s %s;branch=z9hG4bK%s;rport\r\n", transportUpper(cfg.Transport), formatHostPort(cfg.LocalIP), newBranch()))
	b.WriteString(fmt.Sprintf("From: <sip:%s@%s>;tag=%s\r\n", cfg.IMPI, cfg.Domain, newTag()))
	b.WriteString(fmt.Sprintf("To: <sip:%s@%s>\r\n", sanitizePhone(to), cfg.Domain))
	b.WriteString(fmt.Sprintf("Call-ID: %s\r\n", callID))
	b.WriteString("CSeq: 1 MESSAGE\r\n")
	b.WriteString("Content-Type: text/plain\r\n")
	b.WriteString(fmt.Sprintf("X-VoWiFi-Message-ID: %s\r\n", ref))
	b.WriteString(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(text)))
	b.WriteString(text)
	return b.String()
}

// sanitizePhone strips non-digit characters from a phone number.
func sanitizePhone(p string) string {
	var b strings.Builder
	for _, c := range p {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// GetSMSDeliveryStatus returns the delivery status of an SMS.
func (s *Service) GetSMSDeliveryStatus(ctx context.Context, ref string) (*DeliveryStatus, error) {
	if s == nil {
		return nil, errors.New("imscore: nil service")
	}
	if s.delivery == nil {
		return nil, errors.New("imscore: no delivery store")
	}
	return s.delivery.GetSMSDeliveryStatus(ref)
}

// emitSMS publishes SMS events.
func (s *Service) emitSMS(outcome *SMSSendOutcome) {
	if s == nil || s.bus == nil || outcome == nil {
		return
	}
	s.bus.Publish(&events.EventSMSSent{
		DevID:    s.cfg.DeviceID,
		Content:  "sms",
		Time:     time.Now(),
	})
}

// newMessageID generates an SMS message ID.
func newMessageID() string {
	return fmt.Sprintf("sms-%s-%d", randomHex(6), time.Now().UnixNano()%1000000)
}
