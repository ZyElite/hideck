package imscore

import (
	"context"
	"errors"
)

// SIPResponse is the public form of a final SIP transaction response.
type SIPResponse struct {
	StatusCode int
	Reason     string
	Headers    map[string]string
	Body       []byte
}

// SIPTransactionCallbacks receives transaction-layer responses that remain
// relevant outside the initial RoundTrip return.
type SIPTransactionCallbacks struct {
	OnProvisional         func(SIPResponse) error
	OnFinalRetransmission func(SIPResponse) error
}

// RoundTripSIP sends a request and waits for its real final response.
func (s *Service) RoundTripSIP(ctx context.Context, request string) (SIPResponse, error) {
	if s == nil || s.transport == nil {
		return SIPResponse{}, errors.New("imscore: SIP transport is unavailable")
	}
	response, err := s.transport.RoundTrip(ctx, request)
	if err != nil {
		return SIPResponse{}, err
	}
	return publicSIPResponse(response), nil
}

// RoundTripSIPWithProvisional delivers each 1xx response while retaining the
// INVITE transaction until its final response arrives.
func (s *Service) RoundTripSIPWithProvisional(
	ctx context.Context,
	request string,
	onProvisional func(SIPResponse) error,
) (SIPResponse, error) {
	return s.RoundTripSIPWithCallbacks(ctx, request, SIPTransactionCallbacks{
		OnProvisional: onProvisional,
	})
}

// RoundTripSIPWithCallbacks retains the INVITE transaction long enough to
// surface retransmitted final responses as required by Timer M.
func (s *Service) RoundTripSIPWithCallbacks(
	ctx context.Context,
	request string,
	callbacks SIPTransactionCallbacks,
) (SIPResponse, error) {
	if s == nil || s.transport == nil {
		return SIPResponse{}, errors.New("imscore: SIP transport is unavailable")
	}
	response, err := s.transport.roundTripWithCallbacks(ctx, request, transactionCallbacks(callbacks))
	if err != nil {
		return SIPResponse{}, err
	}
	return publicSIPResponse(response), nil
}

func transactionCallbacks(callbacks SIPTransactionCallbacks) sipTransactionCallbacks {
	return sipTransactionCallbacks{
		onProvisional: func(value *sipResponse) error {
			if callbacks.OnProvisional == nil {
				return nil
			}
			return callbacks.OnProvisional(publicSIPResponse(value))
		},
		onFinalRetransmit: func(value *sipResponse) error {
			if callbacks.OnFinalRetransmission == nil {
				return nil
			}
			return callbacks.OnFinalRetransmission(publicSIPResponse(value))
		},
	}
}

func publicSIPResponse(response *sipResponse) SIPResponse {
	if response == nil {
		return SIPResponse{}
	}
	return SIPResponse{
		StatusCode: response.StatusCode, Reason: response.Reason,
		Headers: cloneSIPHeaders(response.Headers), Body: append([]byte(nil), response.Body...),
	}
}
