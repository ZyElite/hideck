package api

import (
	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/internal/notify"
)

type telegramIdentityTransition struct {
	manager        *notify.Manager
	changed        bool
	previousTarget int64
}

func prepareTelegramIdentityTransition(
	manager *notify.Manager,
	previous config.TelegramConfig,
	next config.TelegramConfig,
) (telegramIdentityTransition, error) {
	transition := telegramIdentityTransition{manager: manager}
	if manager == nil || !telegramIdentityChanged(previous, next) {
		return transition, nil
	}
	state, err := manager.LoadRuntimeState()
	if err != nil {
		return transition, err
	}
	transition.changed = true
	transition.previousTarget = state.Telegram.DefaultTarget
	err = manager.UpdateRuntimeState(func(state *notify.RuntimeState) error {
		state.Telegram.DefaultTarget = 0
		return nil
	})
	return transition, err
}

func (t telegramIdentityTransition) rollbackBinding() error {
	if !t.changed {
		return nil
	}
	return t.manager.UpdateRuntimeState(func(state *notify.RuntimeState) error {
		state.Telegram.DefaultTarget = t.previousTarget
		return nil
	})
}

func (t telegramIdentityTransition) revokePreviousChannel() {
	if t.changed {
		t.manager.RevokeChannel("telegram")
	}
}

func (s *Server) telegramBindingStatus() (int64, string) {
	if s.fullCfg.Telegram.ChatID != 0 {
		return s.fullCfg.Telegram.ChatID, ""
	}
	if s.notifyMgr == nil {
		return 0, ""
	}
	state, err := s.notifyMgr.LoadRuntimeState()
	if err != nil {
		return 0, err.Error()
	}
	return state.Telegram.DefaultTarget, ""
}

func telegramIdentityChanged(previous, next config.TelegramConfig) bool {
	return previous.BotToken != next.BotToken || previous.AdminID != next.AdminID ||
		(previous.ChatID != 0 && next.ChatID == 0)
}
