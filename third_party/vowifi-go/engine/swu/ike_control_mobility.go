package swu

import "errors"

type pausedIKEControl struct {
	running bool
}

func (s *Session) pauseIKEControlForMobility() (pausedIKEControl, error) {
	s.controlMu.Lock()
	if !s.controlRunning {
		s.controlMu.Unlock()
		return pausedIKEControl{}, errors.New("swu: IKE control plane is not running")
	}
	stop := s.controlStop
	taskManager := s.taskMgr
	if stop == nil || taskManager == nil {
		s.controlMu.Unlock()
		return pausedIKEControl{}, errors.New("swu: IKE control plane is incomplete")
	}
	close(stop)
	s.controlMu.Unlock()

	taskManager.Stop()
	s.controlWG.Wait()
	s.controlMu.Lock()
	if s.taskMgr == taskManager {
		s.taskMgr = nil
		s.controlRequests = nil
		s.controlStop = nil
		s.controlTransport = nil
		s.controlRunning = false
	}
	s.controlMu.Unlock()
	return pausedIKEControl{running: true}, nil
}

func (s *Session) resumeIKEControlAfterMobility(control pausedIKEControl) error {
	if !control.running {
		return nil
	}
	return s.startIKEControl()
}
