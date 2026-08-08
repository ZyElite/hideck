package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// SIPDialogProfile is the registered signaling endpoint used by IMS dialogs.
type SIPDialogProfile struct {
	LocalURI       string
	ContactURI     string
	Domain         string
	LocalAddress   string
	Transport      string
	ServiceRoute   string
	SecurityVerify string
	PANI           string
	UserAgent      string
}

// SIPResponse is the public form of a final SIP transaction response.
type SIPResponse struct {
	StatusCode int
	Reason     string
	Headers    map[string]string
	Body       []byte
}

// InboundVoiceRequest is a SIP request routed to the active voice agent.
type InboundVoiceRequest struct {
	Method      string
	CallID      string
	From        string
	To          string
	Contact     string
	RecordRoute string
	CSeq        string
	ContentType string
	Body        []byte
	Responder   InboundVoiceResponder
}

// InboundVoiceResponse is one provisional or final response to an inbound
// voice request.
type InboundVoiceResponse struct {
	StatusCode  int
	ContentType string
	Body        []byte
	Contact     string
	ToTag       string
}

// InboundVoiceResponder retains the network transaction used by an inbound
// voice request. Provisional responses may precede exactly one final response.
type InboundVoiceResponder interface {
	Respond(InboundVoiceResponse) error
	LocalTag() string
}

// InboundVoiceResult controls the SIP response for a handled request.
type InboundVoiceResult struct {
	Handled    bool
	StatusCode int
}

// VoiceRequestHandler consumes inbound IMS voice dialog requests.
type VoiceRequestHandler interface {
	HandleInboundVoiceRequest(InboundVoiceRequest) (InboundVoiceResult, error)
}

// SetVoiceRequestHandler installs or removes the active voice router.
func (s *Service) SetVoiceRequestHandler(handler VoiceRequestHandler) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.voiceHandler = handler
	s.mu.Unlock()
}

// RegisteredSIPDialogProfile returns the active IMS registration binding.
func (s *Service) RegisteredSIPDialogProfile() (SIPDialogProfile, error) {
	if s == nil || s.cfg == nil {
		return SIPDialogProfile{}, errors.New("imscore: service is not configured")
	}
	if !s.IsRegistered() {
		return SIPDialogProfile{}, errors.New("imscore: IMS is not registered")
	}
	clientAddress, serverAddress, route, securityVerify, transport := s.smsMessageRoute()
	if clientAddress == "" || serverAddress == "" {
		return SIPDialogProfile{}, errors.New("imscore: registered SIP transport is unavailable")
	}
	localURI := firstPublicIdentity(s.cfg)
	if localURI == "" {
		return SIPDialogProfile{}, errors.New("imscore: registered public identity is unavailable")
	}
	return SIPDialogProfile{
		LocalURI: localURI, Domain: strings.TrimSpace(s.cfg.Domain),
		LocalAddress: clientAddress, Transport: transport, ServiceRoute: route,
		ContactURI:     fmt.Sprintf("sip:%s@%s;transport=%s", contactUser(s.cfg), serverAddress, transport),
		SecurityVerify: securityVerify, PANI: s.GetPAccessNetworkInfo(),
		UserAgent: strings.TrimSpace(s.cfg.UserAgent),
	}, nil
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
	return SIPResponse{
		StatusCode: response.StatusCode, Reason: response.Reason,
		Headers: cloneSIPHeaders(response.Headers), Body: append([]byte(nil), response.Body...),
	}, nil
}

// EventBus returns the service event bus used by lifecycle consumers.
func (s *Service) EventBus() *EventBus {
	if s == nil {
		return nil
	}
	return s.bus
}

func (s *Service) handleInboundVoice(raw string, reply func(string) error) (inboundSIPResult, bool, error) {
	s.mu.RLock()
	handler := s.voiceHandler
	s.mu.RUnlock()
	if handler == nil {
		return inboundSIPResult{}, false, nil
	}
	body, err := rawSIPBody(raw)
	if err != nil {
		return inboundSIPResult{}, true, err
	}
	result, err := handler.HandleInboundVoiceRequest(InboundVoiceRequest{
		Method: sipRequestMethod(raw), CallID: rawSIPHeaderValue(raw, "Call-ID"),
		From: rawSIPHeaderValue(raw, "From"), To: rawSIPHeaderValue(raw, "To"),
		Contact: rawSIPHeaderValue(raw, "Contact"), RecordRoute: rawSIPHeaderValue(raw, "Record-Route"),
		CSeq: rawSIPHeaderValue(raw, "CSeq"), ContentType: rawSIPHeaderValue(raw, "Content-Type"),
		Body: body, Responder: newInboundVoiceResponder(raw, reply),
	})
	if !result.Handled {
		return inboundSIPResult{}, false, err
	}
	if result.StatusCode == 0 {
		return inboundSIPResult{}, true, err
	}
	response, responseErr := buildSIPRequestResponse(raw, result.StatusCode)
	if responseErr != nil {
		return inboundSIPResult{}, true, responseErr
	}
	return inboundSIPResult{response: response}, true, err
}

func cloneSIPHeaders(headers map[string]string) map[string]string {
	copy := make(map[string]string, len(headers))
	for name, value := range headers {
		copy[name] = value
	}
	return copy
}

func splitSIPHeaderValues(value string) []string {
	var values []string
	start, angleDepth := 0, 0
	for index, char := range value {
		switch char {
		case '<':
			angleDepth++
		case '>':
			if angleDepth > 0 {
				angleDepth--
			}
		case ',':
			if angleDepth == 0 {
				if item := strings.TrimSpace(value[start:index]); item != "" {
					values = append(values, item)
				}
				start = index + 1
			}
		}
	}
	if item := strings.TrimSpace(value[start:]); item != "" {
		values = append(values, item)
	}
	return values
}
