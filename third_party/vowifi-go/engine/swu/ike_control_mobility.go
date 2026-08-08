package swu

import "errors"

type pausedIKEControl struct {
	running bool
}

func (s *Session) pauseIKEControlForMobility() (pausedIKEControl, error) {
	stopped, err := s.stopIKEControlGeneration()
	if err != nil {
		return pausedIKEControl{}, err
	}
	if !stopped {
		return pausedIKEControl{}, errors.New("swu: IKE control plane is not running")
	}
	return pausedIKEControl{running: true}, nil
}

func (s *Session) stopIKEControl() error {
	_, err := s.stopIKEControlGeneration()
	return err
}

func (s *Session) stopIKEControlGeneration() (bool, error) {
	s.controlMu.Lock()
	if s.controlStopping {
		s.controlMu.Unlock()
		s.controlWG.Wait()
		return true, nil
	}
	if !s.controlRunning {
		s.controlMu.Unlock()
		return false, nil
	}
	stop := s.controlStop
	taskManager := s.taskMgr
	if stop == nil || taskManager == nil {
		s.controlMu.Unlock()
		return false, errors.New("swu: IKE control plane is incomplete")
	}
	s.controlStopping = true
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
	s.controlStopping = false
	s.controlMu.Unlock()
	return true, nil
}

func (s *Session) resumeIKEControlAfterMobility(control pausedIKEControl) error {
	if !control.running {
		return nil
	}
	return s.startIKEControl()
}
