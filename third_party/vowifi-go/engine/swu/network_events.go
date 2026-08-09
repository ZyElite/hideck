package swu

import (
	"fmt"
	"time"

	"github.com/iniwex5/vowifi-go/engine/ipsec"
	"github.com/iniwex5/vowifi-go/engine/logger"
	"go.uber.org/zap"
)

const (
	preKeyUnreachableThreshold = 3
	preKeyUnreachableWindow    = 5 * time.Second
	minimumIKEPathMTU          = 500
)

type networkEventState struct {
	preKeyCount int
	firstSeen   time.Time
}

// startNetEventMonitor restores the original no-result production monitor.
func (s *Session) startNetEventMonitor() {
	transport := s.transport()
	if transport == nil || transport.NetEventsChan() == nil || s.ctx.Err() != nil {
		return
	}
	events := transport.NetEventsChan()
	s.netEventMu.Lock()
	if s.netEventClosing {
		s.netEventMu.Unlock()
		return
	}
	if s.netEventMonitors == nil {
		s.netEventMonitors = make(map[<-chan ipsec.NetEvent]struct{})
	}
	if _, exists := s.netEventMonitors[events]; exists {
		s.netEventMu.Unlock()
		return
	}
	s.netEventMonitors[events] = struct{}{}
	s.netEventWG.Add(1)
	s.netEventMu.Unlock()
	go s.runNetEventMonitor(events)
}

func (s *Session) runNetEventMonitor(events <-chan ipsec.NetEvent) {
	defer s.netEventWG.Done()
	defer func() {
		s.netEventMu.Lock()
		delete(s.netEventMonitors, events)
		s.netEventMu.Unlock()
	}()
	state := networkEventState{}
	for {
		select {
		case <-s.ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if !s.handleNetworkEvent(event, &state) {
				return
			}
		}
	}
}

func (s *Session) handleNetworkEvent(event ipsec.NetEvent, state *networkEventState) bool {
	switch event.Type {
	case ipsec.EventPathMTU:
		s.applyPathMTU(event.PMTU)
	case ipsec.EventNetworkDown:
		return s.handleNetworkDown(event.Reason, state)
	case ipsec.EventNATPortChanged:
		logger.Info("SWu NAT-T peer port changed",
			zap.Int("old_port", event.OldPort), zap.Int("new_port", event.NewPort))
	}
	return true
}

func (s *Session) applyPathMTU(mtu uint32) {
	s.mu.Lock()
	if mtu >= minimumIKEPathMTU && mtu < s.ikeFragmentMTU {
		s.ikeFragmentMTU = mtu
	}
	s.mu.Unlock()
}

func (s *Session) handleNetworkDown(reason string, state *networkEventState) bool {
	s.mu.RLock()
	ready := s.ikeKeys != nil && s.state == stateEstablished
	s.mu.RUnlock()
	if ready {
		state.preKeyCount, state.firstSeen = 0, time.Time{}
		s.launchNetworkDPD()
		return true
	}
	now := time.Now()
	if state.firstSeen.IsZero() || now.Sub(state.firstSeen) > preKeyUnreachableWindow {
		state.firstSeen, state.preKeyCount = now, 1
	} else {
		state.preKeyCount++
	}
	if state.preKeyCount < preKeyUnreachableThreshold {
		return true
	}
	err := fmt.Errorf("swu: network unreachable before IKE keys (%d events in %s): %s",
		state.preKeyCount, preKeyUnreachableWindow, reason)
	s.failSession(err)
	return false
}

func (s *Session) launchNetworkDPD() {
	s.netEventMu.Lock()
	if s.netEventClosing {
		s.netEventMu.Unlock()
		return
	}
	s.netEventWG.Add(1)
	s.netEventMu.Unlock()
	go func() {
		defer s.netEventWG.Done()
		if err := s.DPDProbe(); err != nil && s.ctx.Err() == nil {
			logger.Warn("SWu network-event DPD failed", zap.Error(err))
		}
	}()
}

func (s *Session) closeNetEventLifecycle() {
	s.netEventMu.Lock()
	s.netEventClosing = true
	s.netEventMu.Unlock()
}
