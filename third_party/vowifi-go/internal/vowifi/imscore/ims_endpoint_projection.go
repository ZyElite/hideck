package imscore

import (
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

// Subscribe restores the v1.5.5 endpoint-level IMS event subscription API.
func (s *Service) Subscribe(
	spec imsendpoint.EventSubscription,
	handler func(imsendpoint.Event),
) func() {
	return s.getIMSEventBus().subscribe(spec, handler)
}

func (s *Service) getIMSEventBus() *imsEventBus {
	if s == nil {
		return nil
	}
	s.imsEventBusOnce.Do(func() {
		s.imsEventBus = newIMSEventBus(s.DeviceID())
	})
	return s.imsEventBus
}

func (s *Service) publishIMSEvent(event imsendpoint.Event) imsEventPublishReceipt {
	if s == nil {
		return imsEventPublishReceipt{}
	}
	if strings.TrimSpace(event.DeviceID) == "" {
		event.DeviceID = s.DeviceID()
	}
	return s.getIMSEventBus().publishWithReceipt(event)
}

func (s *Service) buildIMSEventFromRequest(request *sip.Request) imsendpoint.Event {
	event := imsendpoint.Event{
		DeviceID: s.DeviceID(), Kind: "request", Session: s.Session(),
	}
	if request == nil {
		return event
	}
	event.Method = strings.ToUpper(strings.TrimSpace(string(request.Method)))
	event.Request = request.Clone()
	if request.CSeq() != nil {
		event.CSeqMethod = strings.ToUpper(strings.TrimSpace(string(request.CSeq().MethodName)))
	}
	if request.CallID() != nil {
		event.CallID = strings.TrimSpace(request.CallID().Value())
	}
	return event
}

func (s *Service) buildIMSEventFromResponse(response *sip.Response) imsendpoint.Event {
	event := imsendpoint.Event{
		DeviceID: s.DeviceID(), Kind: "response", Session: s.Session(),
	}
	if response == nil {
		return event
	}
	event.Response = response.Clone()
	event.StatusCode = response.StatusCode
	if response.CSeq() != nil {
		event.CSeqMethod = strings.ToUpper(strings.TrimSpace(string(response.CSeq().MethodName)))
	}
	if response.CallID() != nil {
		event.CallID = strings.TrimSpace(response.CallID().Value())
	}
	return event
}

func (s *Service) buildInboundRequestEvent(
	request *sip.Request,
	transaction *serverSIPTransaction,
) imsendpoint.Event {
	event := s.buildIMSEventFromRequest(request)
	if request == nil {
		event.ResponseAcknowledged = true
		return event
	}
	if request.IsInvite() {
		if transaction != nil {
			event.ServerInvite = newServerInviteHandle(request, transaction)
		}
		return event
	}
	switch strings.ToUpper(strings.TrimSpace(string(request.Method))) {
	case "MESSAGE", "NOTIFY", "OPTIONS", "ACK":
		event.ResponseAcknowledged = true
	default:
		if transaction != nil {
			event.InboundRequest = newInboundRequestHandle(request, transaction)
		}
	}
	return event
}
