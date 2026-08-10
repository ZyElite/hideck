package imscore

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/ussi"
)

// USSDResult is the exact v1.5.5 runtime result contract.
type USSDResult struct {
	Status    int    `json:"status"`
	Text      string `json:"text"`
	RawXML    string `json:"raw_xml,omitempty"`
	DCS       int    `json:"dcs"`
	SessionID string `json:"session_id,omitempty"`
}

func (s *Service) SendUSSD(ctx context.Context, code string) (*USSDResult, error) {
	service, err := s.readyUSSIService()
	if err != nil {
		return nil, err
	}
	result, sendErr := service.Send(ctx, code)
	s.dispatchUSSIResult(code, result)
	return adaptUSSIResult(result), sendErr
}

func (s *Service) ContinueUSSD(
	ctx context.Context,
	sessionID, input string,
) (*USSDResult, error) {
	service, err := s.readyUSSIService()
	if err != nil {
		return nil, err
	}
	result, continueErr := service.Continue(ctx, sessionID, input)
	s.dispatchUSSIResult(input, result)
	return adaptUSSIResult(result), continueErr
}

func (s *Service) CancelUSSD(ctx context.Context, sessionID string) error {
	service := s.ussiService()
	if service == nil {
		return errors.New("imscore: USSD not available")
	}
	return service.Cancel(ctx, sessionID)
}

func (s *Service) GetActiveUSSDSession() string {
	service := s.existingUSSIService()
	if service == nil {
		return ""
	}
	return service.ActiveSessionID()
}

func (s *Service) existingUSSIService() *ussi.Service {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ussd
}

func (s *Service) readyUSSIService() (*ussi.Service, error) {
	if s == nil {
		return nil, errors.New("imscore: USSD not available")
	}
	if !s.IsRegistered() {
		return nil, errors.New("imscore: IMS is not registered")
	}
	s.mu.RLock()
	hasSession := s.regSession != nil
	s.mu.RUnlock()
	if !hasSession {
		return nil, errors.New("imscore: registered SIP session is unavailable")
	}
	service := s.ussiService()
	if service == nil {
		return nil, errors.New("imscore: USSD not available")
	}
	return service, nil
}

func (s *Service) ussiService() *ussi.Service {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ussd == nil {
		deviceID := ""
		if s.cfg != nil {
			deviceID = s.cfg.DeviceID
		}
		s.ussd = ussi.NewService(deviceID, s)
	}
	return s.ussd
}

func (s *Service) dispatchUSSIResult(command string, result *ussi.Result) {
	if s == nil || s.bus == nil || result == nil {
		return
	}
	deviceID := ""
	if s.cfg != nil {
		deviceID = s.cfg.DeviceID
	}
	s.bus.Publish(events.EventUSSDResult{
		DevID: deviceID, SessionID: result.SessionID,
		Command: command, Text: result.Text, Status: result.Status, Time: time.Now(),
		Code: strconv.Itoa(result.Status), Message: result.Text,
	})
}

func adaptUSSIResult(result *ussi.Result) *USSDResult {
	if result == nil {
		return nil
	}
	return &USSDResult{
		Status: result.Status, Text: result.Text, RawXML: result.RawXML,
		DCS: result.DCS, SessionID: result.SessionID,
	}
}

func (s *Service) handleInboundUSSI(raw string) (inboundSIPResult, bool, error) {
	service := s.ussiService()
	if service == nil {
		return inboundSIPResult{}, false, nil
	}
	message, err := parseSIPMessage(raw)
	if err != nil {
		return inboundSIPResult{}, false, nil
	}
	request, ok := message.(*sip.Request)
	if !ok {
		return inboundSIPResult{}, false, nil
	}
	handled := false
	switch request.Method {
	case sip.INFO:
		handled = service.HandleInboundInfoNoResponse(context.Background(), request)
	case sip.BYE:
		handled = service.HandleInboundByeNoResponse(context.Background(), request)
	}
	if !handled {
		return inboundSIPResult{}, false, nil
	}
	response, err := buildSIPRequestResponse(raw, 200)
	if err != nil {
		return inboundSIPResult{}, true, err
	}
	return inboundSIPResult{response: response}, true, nil
}
