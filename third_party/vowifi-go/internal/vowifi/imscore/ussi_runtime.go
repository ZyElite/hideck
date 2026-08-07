package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/ussi"
)

// USSDResult is the compatibility result returned by the runtime adapter.
type USSDResult struct {
	SessionID string
	Code      string
	Message   string
	RawXML    string
	Done      bool
}

func (s *Service) SendUSSD(ctx context.Context, code string) (*USSDResult, error) {
	if err := s.prepareUSSI(); err != nil {
		return nil, err
	}
	result, err := s.ussd.SendContext(ctx, code)
	return adaptUSSIResult(result), err
}

func (s *Service) ContinueUSSD(ctx context.Context, sessionID, input string) (*USSDResult, error) {
	if s == nil || s.ussd == nil {
		return nil, errors.New("imscore: USSD not available")
	}
	result, err := s.ussd.ContinueContext(ctx, sessionID, input)
	return adaptUSSIResult(result), err
}

func (s *Service) CancelUSSD(ctx context.Context, sessionID string) error {
	if s == nil || s.ussd == nil {
		return errors.New("imscore: USSD not available")
	}
	return s.ussd.CancelContext(ctx, sessionID)
}

func (s *Service) GetActiveUSSDSession() string {
	if s == nil || s.ussd == nil {
		return ""
	}
	return s.ussd.ActiveSessionID()
}

func (s *Service) prepareUSSI() error {
	if s == nil || s.cfg == nil || s.ussd == nil {
		return errors.New("imscore: USSD not available")
	}
	if !s.IsRegistered() {
		return errors.New("imscore: IMS is not registered")
	}
	clientAddress, serverAddress, route, securityVerify, transport := s.smsMessageRoute()
	localURI := firstPublicIdentity(s.cfg)
	if localURI == "" {
		return errors.New("imscore: no public identity for USSD")
	}
	contact := fmt.Sprintf("sip:%s@%s;transport=%s", contactUser(s.cfg), serverAddress, transport)
	return s.ussd.Configure(ussi.Config{
		Transport: &ussiTransportAdapter{transport: s.transport},
		LocalURI:  localURI, ContactURI: contact, Domain: s.cfg.Domain,
		LocalAddress: clientAddress, SIPTransport: transport, ServiceRoute: route,
		SecurityVerify: securityVerify, PANI: s.GetPAccessNetworkInfo(),
		UserAgent: strings.TrimSpace(s.cfg.UserAgent), OnResult: s.publishUSSIResult,
	})
}

func firstPublicIdentity(cfg *IMSConfig) string {
	if cfg == nil {
		return ""
	}
	for _, identity := range cfg.IMPU {
		if identity = strings.TrimSpace(identity); identity != "" {
			if strings.HasPrefix(strings.ToLower(identity), "sip:") {
				return identity
			}
			return "sip:" + identity
		}
	}
	if strings.TrimSpace(cfg.IMPI) != "" {
		return "sip:" + strings.TrimSpace(cfg.IMPI)
	}
	return ""
}

func (s *Service) publishUSSIResult(result ussi.Result) {
	if s == nil || s.bus == nil {
		return
	}
	s.bus.Publish(&events.EventUSSDResult{
		DevID: s.cfg.DeviceID, SessionID: result.SessionID,
		Code: result.Code, Message: result.Message,
	})
}

func adaptUSSIResult(result *ussi.Result) *USSDResult {
	if result == nil {
		return nil
	}
	return &USSDResult{
		SessionID: result.SessionID, Code: result.Code, Message: result.Message,
		RawXML: result.RawXML, Done: result.Done,
	}
}

type ussiTransportAdapter struct {
	transport *sipTransport
}

func (a *ussiTransportAdapter) RoundTrip(ctx context.Context, request string) (ussi.Response, error) {
	if a == nil || a.transport == nil {
		return ussi.Response{}, errors.New("imscore: no SIP transport for USSD")
	}
	response, err := a.transport.RoundTrip(ctx, request)
	if err != nil {
		return ussi.Response{}, err
	}
	return ussi.Response{
		StatusCode: response.StatusCode, Reason: response.Reason,
		Headers: response.Headers, Body: append([]byte(nil), response.Body...),
	}, nil
}

func (a *ussiTransportAdapter) Send(_ context.Context, request string) error {
	if a == nil || a.transport == nil {
		return errors.New("imscore: no SIP transport for USSD")
	}
	return a.transport.Send(request)
}

func (s *Service) handleInboundUSSI(raw string) (inboundSIPResult, bool, error) {
	if s == nil || s.ussd == nil {
		return inboundSIPResult{}, false, nil
	}
	body, err := rawSIPBody(raw)
	if err != nil {
		return inboundSIPResult{}, true, err
	}
	result, err := s.ussd.HandleInbound(ussi.InboundRequest{
		Method: sipRequestMethod(raw), CallID: rawSIPHeaderValue(raw, "Call-ID"),
		ContentType: rawSIPHeaderValue(raw, "Content-Type"),
		InfoPackage: rawSIPHeaderValue(raw, "Info-Package"), Body: body,
	})
	if !result.Handled {
		return inboundSIPResult{}, false, err
	}
	status := result.StatusCode
	if status == 0 {
		status = 500
	}
	response, responseErr := buildSIPRequestResponse(raw, status)
	if responseErr != nil {
		return inboundSIPResult{}, true, responseErr
	}
	return inboundSIPResult{response: response}, true, err
}
