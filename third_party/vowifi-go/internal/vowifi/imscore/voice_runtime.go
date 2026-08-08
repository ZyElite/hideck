package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imsheaders"
)

// SIPDialogProfile is the registered signaling endpoint used by IMS dialogs.
type SIPDialogProfile struct {
	LocalURI       string
	ContactURI     string
	ContactHeader  string
	Domain         string
	LocalAddress   string
	Transport      string
	ServiceRoute   string
	SecurityVerify string
	PANI           string
	UserAgent      string
	InitialCSeq    int
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
	Method         string
	CallID         string
	From           string
	To             string
	Contact        string
	RecordRoute    string
	CSeq           string
	ContentType    string
	SessionExpires string
	Body           []byte
	Responder      InboundVoiceResponder
}

// InboundVoiceResponse is one provisional or final response to an inbound
// voice request.
type InboundVoiceResponse struct {
	StatusCode     int
	ContentType    string
	Body           []byte
	Contact        string
	ToTag          string
	SessionExpires string
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
	return s.reserveRegisteredSIPProfile()
}

func (s *Service) reserveRegisteredSIPProfile() (SIPDialogProfile, error) {
	if s == nil || s.cfg == nil {
		return SIPDialogProfile{}, errors.New("imscore: service is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.regState != regRegistered {
		return SIPDialogProfile{}, errors.New("imscore: IMS is not registered")
	}
	session := s.regSession
	if session == nil {
		return SIPDialogProfile{}, errors.New("imscore: registered SIP session is unavailable")
	}
	localURI := strings.TrimSpace(session.publicID)
	registeredContactUser := strings.TrimSpace(session.contactUser)
	if localURI == "" {
		return SIPDialogProfile{}, errors.New("imscore: registered public identity is unavailable")
	}
	if registeredContactUser == "" {
		return SIPDialogProfile{}, errors.New("imscore: registered Contact identity is unavailable")
	}
	route := s.registeredSIPRouteLocked()
	if !route.live {
		return SIPDialogProfile{}, errors.New("imscore: registered SIP transport is not connected")
	}
	if route.clientAddress == "" || route.serverAddress == "" {
		return SIPDialogProfile{}, errors.New("imscore: registered SIP transport is unavailable")
	}
	initialCSeq := s.reserveSIPCSeqLocked(session, route.securityVerify != "")
	contactURI, contactHeader := registeredVoiceContact(s.cfg, registeredContactUser, route.serverAddress)
	return SIPDialogProfile{
		LocalURI: localURI, Domain: strings.TrimSpace(s.cfg.Domain),
		LocalAddress: route.clientAddress, Transport: route.transport, ServiceRoute: route.serviceRoute,
		ContactURI: contactURI, ContactHeader: contactHeader,
		SecurityVerify: route.securityVerify, PANI: s.GetPAccessNetworkInfo(),
		UserAgent: strings.TrimSpace(s.cfg.UserAgent), InitialCSeq: initialCSeq,
	}, nil
}

func (s *Service) reserveSIPCSeqLocked(session *registerSession, subscriptionConsumed bool) int {
	minimum := session.cseq + 1
	if subscriptionConsumed {
		minimum += 2
	}
	if s.nextSIPCSeq < minimum {
		s.nextSIPCSeq = minimum
		return minimum
	}
	s.nextSIPCSeq++
	return s.nextSIPCSeq
}

func registeredVoiceContact(cfg *IMSConfig, user, address string) (string, string) {
	uri := fmt.Sprintf("sip:%s@%s", user, address)
	template := cfg.RegisterTemplate
	if len(template.ContactOrder) == 0 {
		return uri, "<" + uri + ">"
	}
	instance := strings.TrimSpace(cfg.IMEI)
	if instance == "" {
		instance = strings.TrimSpace(cfg.DeviceID)
	}
	header := imsheaders.IMSContactURI(uri, imsheaders.IMSContactOptions{
		AccessType: template.AccessType, Instance: instance,
		ICSIRef: template.ICSIRef, ParamOrder: template.ContactOrder,
	})
	return uri, header
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
	if s == nil || s.transport == nil {
		return SIPResponse{}, errors.New("imscore: SIP transport is unavailable")
	}
	response, err := s.transport.roundTripWithProvisional(ctx, request, func(value *sipResponse) error {
		if onProvisional == nil {
			return nil
		}
		return onProvisional(publicSIPResponse(value))
	})
	if err != nil {
		return SIPResponse{}, err
	}
	return publicSIPResponse(response), nil
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
		SessionExpires: rawSIPHeaderValue(raw, "Session-Expires"),
		Body:           body, Responder: newInboundVoiceResponder(raw, reply),
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
