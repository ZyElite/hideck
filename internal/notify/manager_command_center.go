package notify

const commandCenterUnavailableReply = "命令执行失败\n原因    命令中心尚未就绪"

func (m *Manager) SetChannelCommandExecutor(executor ChannelCommandExecutor) {
	m.commandExecutorMu.Lock()
	m.commandExecutor = executor
	m.commandExecutorMu.Unlock()
}

func (m *Manager) channelCommandExecutor() ChannelCommandExecutor {
	m.commandExecutorMu.RLock()
	defer m.commandExecutorMu.RUnlock()
	return m.commandExecutor
}

func (m *Manager) channelCommandHandler(channel, name string, handler CommandHandler) CommandHandler {
	return func(ctx CommandContext, arguments []string) string {
		executor := m.channelCommandExecutor()
		if executor == nil {
			return commandCenterUnavailableReply
		}
		request := ChannelCommandRequest{
			Channel:   channel,
			Name:      name,
			Arguments: append([]string(nil), arguments...),
		}
		return executor.ExecuteChannelCommand(ctx, request, handler)
	}
}
