package swu

import "github.com/iniwex5/vowifi-go/engine/ipsec"

func (s *Session) transport() ipsec.Transport {
	if s == nil {
		return nil
	}
	s.transportMu.RLock()
	defer s.transportMu.RUnlock()
	return s.socket
}

func (s *Session) setTransport(transport ipsec.Transport) {
	s.transportMu.Lock()
	s.socket = transport
	s.transportMu.Unlock()
}

func (s *Session) takeTransport() ipsec.Transport {
	s.transportMu.Lock()
	transport := s.socket
	s.socket = nil
	s.transportMu.Unlock()
	return transport
}

func (s *Session) replacementTransport(previous ipsec.Transport) ipsec.Transport {
	current := s.transport()
	if current == nil || previous == nil {
		return nil
	}
	if current.ESPPackets() == previous.ESPPackets() {
		return nil
	}
	return current
}
