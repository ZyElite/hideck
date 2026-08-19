package api

import (
	"errors"
	"net/url"
	"strings"

	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/pkg/logger"
)

const defaultWeixinBaseURL = "https://ilinkai.weixin.qq.com"

type weixinNotificationSettings struct {
	Enabled         bool     `json:"enabled"`
	BaseURL         string   `json:"base_url"`
	AllowedUserIDs  []string `json:"allowed_user_ids"`
	AllowedGroupIDs []string `json:"allowed_group_ids"`
}

func (s *Server) weixinSettingsForResponse() weixinNotificationSettings {
	settings := weixinNotificationSettings{
		Enabled:         s.fullCfg.Weixin.Enabled,
		BaseURL:         s.fullCfg.Weixin.BaseURL,
		AllowedUserIDs:  append([]string(nil), s.fullCfg.Weixin.AllowedUserIDs...),
		AllowedGroupIDs: append([]string(nil), s.fullCfg.Weixin.AllowedGroupIDs...),
	}
	settings.AllowedUserIDs = s.mergeWeixinBoundUserIDs(settings.AllowedUserIDs)
	s.persistMissingAllowedUserIDs("weixin", settings.AllowedUserIDs)
	return settings
}

func (s *Server) mergeWeixinBoundUserIDs(ids []string) []string {
	if s == nil || s.notifyMgr == nil {
		return normalizeStringList(ids)
	}
	state, err := s.notifyMgr.LoadRuntimeState()
	if err != nil {
		return normalizeStringList(ids)
	}
	ids = append(ids, state.Weixin.AllowedUsers...)
	ids = append(ids, state.Weixin.UserID, state.Weixin.DefaultTarget)
	return normalizeStringList(ids)
}

func (s *Server) mergeWeComBotBoundUserIDs(ids []string) []string {
	if s == nil || s.notifyMgr == nil {
		return normalizeStringList(ids)
	}
	state, err := s.notifyMgr.LoadRuntimeState()
	if err != nil {
		return normalizeStringList(ids)
	}
	ids = append(ids, state.WeComBot.AllowedUsers...)
	ids = append(ids, state.WeComBot.DefaultTarget)
	return normalizeStringList(ids)
}

func (s *Server) persistMissingAllowedUserIDs(channel string, merged []string) {
	if s == nil || s.fullCfg == nil || strings.TrimSpace(s.configPath) == "" || len(merged) == 0 {
		return
	}
	var current []string
	switch channel {
	case "weixin":
		current = s.fullCfg.Weixin.AllowedUserIDs
	case "wecom_bot":
		current = s.fullCfg.WeComBot.AllowedUserIDs
	default:
		return
	}
	if len(normalizeStringList(current)) > 0 {
		return
	}
	nextConfig := *s.fullCfg
	switch channel {
	case "weixin":
		nextConfig.Weixin.AllowedUserIDs = append([]string(nil), merged...)
	case "wecom_bot":
		nextConfig.WeComBot.AllowedUserIDs = append([]string(nil), merged...)
	}
	if err := config.UpdateNotificationInFile(s.configPath, notificationConfigsFrom(&nextConfig)); err != nil {
		logger.Warn("回写通知绑定用户失败", "channel", channel, "err", err)
		return
	}
	switch channel {
	case "weixin":
		s.fullCfg.Weixin.AllowedUserIDs = nextConfig.Weixin.AllowedUserIDs
	case "wecom_bot":
		s.fullCfg.WeComBot.AllowedUserIDs = nextConfig.WeComBot.AllowedUserIDs
	}
}

func buildWeixinConfig(
	request *weixinNotificationSettings, current config.WeixinConfig,
) (config.WeixinConfig, error) {
	if request == nil {
		return current, nil
	}
	baseURL := strings.TrimRight(strings.TrimSpace(request.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultWeixinBaseURL
	}
	if err := validateWeixinBaseURL(baseURL); err != nil {
		return config.WeixinConfig{}, err
	}
	return config.WeixinConfig{
		Enabled: request.Enabled, BaseURL: baseURL, CDNBaseURL: current.CDNBaseURL,
		AllowedUserIDs:  normalizeStringList(request.AllowedUserIDs),
		AllowedGroupIDs: normalizeStringList(request.AllowedGroupIDs),
	}, nil
}

func validateWeixinBaseURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("个人微信 iLink 地址无效")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname()) {
		return nil
	}
	return errors.New("个人微信 iLink 地址必须使用 HTTPS；本机测试可使用 HTTP")
}
