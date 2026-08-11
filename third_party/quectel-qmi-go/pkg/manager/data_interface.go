package manager

// DataInterface returns the Linux interface carrying the active data session.
// QMAP sessions use the mux interface; non-QMAP sessions use the modem interface.
func (m *Manager) DataInterface() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.muxIface != "" {
		return m.muxIface
	}
	return m.cfg.Device.NetInterface
}
