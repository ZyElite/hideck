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
