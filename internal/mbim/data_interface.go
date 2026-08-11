package mbimcore

func (m *Manager) DataInterface() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dataCfg.Interface
}
