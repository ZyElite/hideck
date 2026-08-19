package api

import (
	"strings"

	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/internal/notify"
	"github.com/yibaiba/hideck/pkg/logger"
)

func (s *Server) mergeFeishuBoundChatIDs(ids []string) []string {
	cfg := config.FeishuConfig{ChatIDs: ids}
	if s != nil && s.fullCfg != nil {
		cfg.ChatID = s.fullCfg.Feishu.ChatID
	}
	if s == nil || s.notifyMgr == nil {
		return notify.TrustedFeishuBindingFrom(cfg, notify.FeishuRuntimeState{}).ChatIDs
	}
	state, err := s.notifyMgr.LoadRuntimeState()
	if err != nil {
		return notify.TrustedFeishuBindingFrom(cfg, notify.FeishuRuntimeState{}).ChatIDs
	}
	return notify.TrustedFeishuBindingFrom(cfg, state.Feishu).ChatIDs
}

func (s *Server) persistMissingFeishuChatIDs(merged []string) {
	if s == nil || s.fullCfg == nil || strings.TrimSpace(s.configPath) == "" || len(merged) == 0 {
		return
	}
	if len(normalizeStringList(s.fullCfg.Feishu.ChatIDs)) > 0 {
		return
	}
	nextConfig := *s.fullCfg
	nextConfig.Feishu.ChatIDs = append([]string(nil), merged...)
	if err := config.UpdateNotificationInFile(s.configPath, notificationConfigsFrom(&nextConfig)); err != nil {
		logger.Warn("回写飞书 Chat ID 失败", "err", err)
		return
	}
	s.fullCfg.Feishu.ChatIDs = nextConfig.Feishu.ChatIDs
}
