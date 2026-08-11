package qmicore

func (m *Manager) DataInterface() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	core := m.qmiMgr
	fallback := m.cfg.Interface
	m.mu.Unlock()
	if core == nil {
		return fallback
	}
	return core.DataInterface()
}
