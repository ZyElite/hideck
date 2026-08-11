package modem

type runtimeRole uint8

const (
	runtimeRoleDefault runtimeRole = iota
	runtimeRoleSMSAuxiliary
)

func (m *Manager) runsATRuntime() bool {
	return m.role == runtimeRoleSMSAuxiliary || !m.pureQMIBackend()
}

func (m *Manager) IsSMSAuxiliary() bool {
	return m != nil && m.role == runtimeRoleSMSAuxiliary
}

func (m *Manager) initializationCommands() []string {
	commands := []string{
		"ATE0",
		"AT+CMGF=0",
		"AT+CNMI=2,1,0,0,0",
	}
	if m.role == runtimeRoleSMSAuxiliary {
		return commands
	}
	return append(commands,
		"AT+CLIP=1",
		"AT+QPCMV=1,2",
	)
}
