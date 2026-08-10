package imscore

import (
	"context"
	"errors"

	"github.com/iniwex5/vowifi-go/internal/vowifi/smsdelivery"
)

// SendOptions is the v1.5.5 SMS encoding policy.
type SendOptions struct {
	Encoding string
}

// SendOutcome preserves the v1.5.5 value shape returned by SMS sends.
type SendOutcome = smsdelivery.SendOutcome

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
func (s *Service) SendSMSWithResult(ctx context.Context, to, text string) (SendOutcome, error) {
	return s.SendSMSWithOptions(ctx, to, text, SendOptions{})
}

// SendSMSWithOptions sends an SMS with options.
func (s *Service) SendSMSWithOptions(ctx context.Context, to, text string, opts SendOptions) (SendOutcome, error) {
	return s.sendOutboundSMS(ctx, to, text, opts)
}

// SendSMSWithDetailedResult preserves the additive pointer result API.
func (s *Service) SendSMSWithDetailedResult(ctx context.Context, to, text string) (*SMSSendOutcome, error) {
	return s.SendSMSWithDetailedOptions(ctx, to, text, SMSSendOptions{})
}

// SendSMSWithDetailedOptions preserves additive suppression and result fields.
func (s *Service) SendSMSWithDetailedOptions(
	ctx context.Context,
	to, text string,
	opts SMSSendOptions,
) (*SMSSendOutcome, error) {
	ctx = withSuppressTGSuccess(ctx, opts.SuppressSendTGSuccess)
	outcome, err := s.SendSMSWithOptions(ctx, to, text, SendOptions{Encoding: opts.Encoding})
	detailed := &SMSSendOutcome{
		Ref: outcome.MessageID, Err: err, MessageID: outcome.MessageID,
		PartsTotal: outcome.PartsTotal, State: outcome.DeliveryState,
	}
	return detailed, err
}

// GetSMSDeliveryStatus returns the delivery status of an SMS.
func (s *Service) GetSMSDeliveryStatus(ref string) (*DeliveryStatus, error) {
	if s == nil {
		return nil, errors.New("imscore: nil service")
	}
	if s.delivery == nil {
		return nil, errors.New("imscore: no delivery store")
	}
	return s.delivery.GetSMSDeliveryStatus(ref)
}

// GetSMSDeliveryStatusContext preserves the additive context-aware API.
func (s *Service) GetSMSDeliveryStatusContext(ctx context.Context, ref string) (*DeliveryStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return s.GetSMSDeliveryStatus(ref)
	}
}
