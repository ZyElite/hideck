package imscore

import "go.uber.org/zap"

const (
	registrationUnregistered int32 = iota
	registrationRegistered
	registrationRejectedTemporary
	registrationRejectedPermanent
	registrationStopping
	registrationStopped
)

var registrationTransitions = map[int32][]int32{
	registrationUnregistered:      {registrationRegistered, registrationRejectedTemporary, registrationRejectedPermanent, registrationStopping},
	registrationRegistered:        {registrationRejectedTemporary, registrationRejectedPermanent, registrationStopping},
	registrationRejectedTemporary: {registrationRegistered, registrationStopping},
	registrationRejectedPermanent: {registrationRegistered, registrationStopping},
	registrationStopping:          {registrationStopped, registrationUnregistered},
	registrationStopped:           nil,
}

func registrationStatusText(status int32) string {
	switch status {
	case registrationUnregistered:
		return "Unregistered"
	case registrationRegistered:
		return "Registered"
	case registrationRejectedTemporary:
		return "RejectedTemporary"
	case registrationRejectedPermanent:
		return "RejectedPermanent"
	case registrationStopping:
		return "Stopping"
	case registrationStopped:
		return "Stopped"
	default:
		return "Unknown"
	}
}

func (s *Service) transitionRegStatus(next int32) bool {
	if s == nil {
		return false
	}
	current := s.regStatus.Load()
	if current == next {
		return true
	}
	for _, allowed := range registrationTransitions[current] {
		if allowed != next {
			continue
		}
		s.regStatus.Store(next)
		return true
	}
	zap.L().Sugar().Warnw("invalid IMS registration status transition",
		"from", registrationStatusText(current), "to", registrationStatusText(next))
	return false
}
