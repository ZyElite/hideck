package api

import (
	"strings"

	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/pkg/logger"
)

func (s *Server) mergeFeishuBoundChatIDs(ids []string) []string {
	if s == nil || s.notifyMgr == nil {
		return normalizeStringList(ids)
	}
	state, err := s.notifyMgr.LoadRuntimeState()
	if err != nil {
		return normalizeStringList(ids)
	}
	return normalizeStringList(append(append([]string(nil), ids...), state.Feishu.ChatIDs...))
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
