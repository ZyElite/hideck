package swu

func (s *Session) ensureWiresharkDebugger() error {
	if s == nil || s.cfg == nil || !s.cfg.EnableWiresharkKeyLog && !s.cfg.Wireshark {
		return nil
	}
	s.debugMu.Lock()
	defer s.debugMu.Unlock()
	if s.debug != nil {
		return nil
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	debugger, err := NewWiresharkDebugger(true, s.cfg.WiresharkKeyLogPath)
	if err != nil {
		return err
	}
	s.debug = debugger
	return nil
}

func (s *Session) currentWiresharkDebugger() *WiresharkDebugger {
	if s == nil {
		return nil
	}
	s.debugMu.Lock()
	defer s.debugMu.Unlock()
	return s.debug
}

func (s *Session) closeWiresharkDebugger() error {
	if s == nil {
		return nil
	}
	s.debugMu.Lock()
	debugger := s.debug
	s.debug = nil
	s.debugMu.Unlock()
	return debugger.Close()
}

func (s *Session) logIKEKeys(keys *IKEKeys) {
	debugger := s.currentWiresharkDebugger()
	if debugger == nil || keys == nil {
		return
	}
	debugger.LogIKESAKeys(
		ikeSPIUint64(s.spiI), ikeSPIUint64(s.spiR),
		keys.SK_ei, keys.SK_er, keys.SK_ai, keys.SK_ar,
		s.encrAlg, s.integAlg,
	)
}

func (s *Session) logChildKeys(keys *childSAKeys) {
	debugger := s.currentWiresharkDebugger()
	if debugger == nil || keys == nil {
		return
	}
	local, remote := configuredLocalIP(s.cfg), s.remoteIP
	transport := s.transport()
	if transport != nil {
		local, remote = transport.LocalIP(), transport.RemoteIP()
	}
	debugger.LogChildSA(
		s.espLocalSPI, s.espRemoteSPI, local.String(), remote.String(),
		keys.responder.enc, keys.initiator.enc, s.espCipher,
	)
}

func childKeysFromRuntime(runtime *childSARuntime) *childSAKeys {
	if runtime == nil {
		return nil
	}
	return &childSAKeys{initiator: runtime.outboundKeys, responder: runtime.inboundKeys}
}
