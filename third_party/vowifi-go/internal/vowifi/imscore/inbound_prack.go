package imscore

import (
	"context"
	"errors"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

func (s *Service) handleIMSPRACK(
	request *sip.Request,
	transaction *serverSIPTransaction,
) error {
	if request == nil {
		return errors.New("imscore: inbound PRACK is empty")
	}
	if transaction == nil {
		return errors.New("imscore: inbound PRACK transaction is unavailable")
	}
	handle := newInboundRequestHandle(request, transaction)
	err := s.RespondInboundRequest(
		context.Background(),
		s.DeviceID(),
		handle,
		imsendpoint.InboundResponseOptions{Code: 200},
	)
	event := s.buildIMSEventFromRequest(request)
	event.InboundRequest = handle
	event.ResponseAcknowledged = err == nil
	s.publishIMSEvent(event)
	return err
}
