package imscore

import (
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

func (s *Service) triggerRegisterImmediate(reason string) {
	if s == nil || s.stopped() {
		return
	}
	now := time.Now()
	s.mu.Lock()
	if s.regState != regRegistered {
		s.mu.Unlock()
		return
	}
	s.registrationRefreshAt = now
	s.nextRegister = now
	s.mu.Unlock()
	s.reRegisterPending.Store(true)
	logging.WarnRate("ims-register-immediate-"+s.DeviceID(), 30*time.Second,
		"IMS immediate re-registration scheduled",
		"device", s.DeviceID(), "reason", strings.TrimSpace(reason))
	s.signalIMSMaintenance()
}
