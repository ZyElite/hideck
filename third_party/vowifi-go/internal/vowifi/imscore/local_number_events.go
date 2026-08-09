package imscore

import (
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
)

func (s *Service) publishLocalNumberLearned(number, source string) {
	if s == nil || s.cfg == nil || s.bus == nil {
		return
	}
	number = strings.TrimSpace(number)
	if number == "" {
		return
	}
	s.bus.Publish(events.EventLocalNumberLearned{
		DevID: s.cfg.DeviceID, IMSI: strings.TrimSpace(s.cfg.IMSI), Number: number,
		Source: strings.TrimSpace(source), Time: time.Now(),
	})
}
