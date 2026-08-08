package imscore

import "strings"

const (
	smsReadyReasonReady             = "IMS SMS receiver ready"
	smsReadyReasonNotRegistered     = "IMS registration is not ready"
	smsReadyReasonProfileNotReady   = "IMS registered identity is not ready"
	smsReadyReasonTransportNotReady = "IMS registered signaling transport is not ready"
	smsReadyReasonReceiverNotReady  = "IMS SMS receiver is not ready"
	smsReadyReasonSMSCNotConfigured = "IMS SMSC is not configured"
)

// SMSReadiness returns a consistent snapshot of the SMS prerequisites.
func (s *Service) SMSReadiness() SMSReadiness {
	if s == nil {
		return evaluateSMSReadiness(false, false, false, false, "")
	}
	s.mu.RLock()
	registered := s.regState == regRegistered
	profileReady := registered && s.regSession != nil &&
		strings.TrimSpace(s.regSession.publicID) != "" &&
		strings.TrimSpace(s.regSession.contactUser) != ""
	transportReady := registered && s.registeredSIPTransportReadyLocked()
	receiverReady := s.smsReceiverReady
	smsc := ""
	if s.cfg != nil {
		smsc = s.cfg.SMSC
	}
	s.mu.RUnlock()
	return evaluateSMSReadiness(registered, profileReady, transportReady, receiverReady, smsc)
}

// SetOnSMSReadinessChanged installs the readiness observer and immediately
// publishes a snapshot so callers cannot miss startup transitions.
func (s *Service) SetOnSMSReadinessChanged(fn func(SMSReadiness)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.onSMSReadiness = fn
	s.mu.Unlock()
	if fn != nil {
		fn(s.SMSReadiness())
	}
}

func (s *Service) setSMSReceiverReady(ready bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	changed := s.smsReceiverReady != ready
	s.smsReceiverReady = ready
	callback := s.onSMSReadiness
	s.mu.Unlock()
	if changed && callback != nil {
		callback(s.SMSReadiness())
	}
}

func (s *Service) notifySMSReadiness() {
	if s == nil {
		return
	}
	s.mu.RLock()
	callback := s.onSMSReadiness
	s.mu.RUnlock()
	if callback != nil {
		callback(s.SMSReadiness())
	}
}

func evaluateSMSReadiness(registered, profileReady, transportReady, receiverReady bool, smsc string) SMSReadiness {
	readiness := SMSReadiness{
		Registered:     registered,
		ProfileReady:   profileReady,
		TransportReady: transportReady,
		ReceiverReady:  receiverReady,
		SMSCPresent:    strings.TrimSpace(smsc) != "",
	}
	switch {
	case !readiness.Registered:
		readiness.Reason = smsReadyReasonNotRegistered
	case !readiness.ProfileReady:
		readiness.Reason = smsReadyReasonProfileNotReady
	case !readiness.TransportReady:
		readiness.Reason = smsReadyReasonTransportNotReady
	case !readiness.ReceiverReady:
		readiness.Reason = smsReadyReasonReceiverNotReady
	case !readiness.SMSCPresent:
		readiness.Reason = smsReadyReasonSMSCNotConfigured
	default:
		readiness.Ready = true
		readiness.Reason = smsReadyReasonReady
	}
	return readiness
}
