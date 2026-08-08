package imscore

import (
	"context"
	"errors"
	"fmt"
	"time"
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
	return s.sendOutboundSMS(ctx, to, text, opts)
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

// newMessageID generates an SMS message ID.
func newMessageID() string {
	return fmt.Sprintf("sms-%s-%d", randomHex(6), time.Now().UnixNano()%1000000)
}
