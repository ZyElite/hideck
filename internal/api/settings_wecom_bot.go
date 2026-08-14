package api

import (
	"errors"
	"net"
	"net/url"
	"strings"

	"github.com/yibaiba/hideck/internal/config"
)

const defaultWeComBotWebSocketURL = "wss://openws.work.weixin.qq.com"

type weComBotNotificationSettings struct {
	Enabled         bool     `json:"enabled"`
	BotID           string   `json:"bot_id"`
	Secret          string   `json:"secret"`
	WebSocketURL    string   `json:"websocket_url"`
	AllowedUserIDs  []string `json:"allowed_user_ids"`
	AllowedGroupIDs []string `json:"allowed_group_ids"`
}

func maskedWeComBotSettings(current config.WeComBotConfig) weComBotNotificationSettings {
	settings := weComBotNotificationSettings{
		Enabled: current.Enabled, BotID: current.BotID, WebSocketURL: current.WebSocketURL,
		AllowedUserIDs:  normalizeStringList(current.AllowedUserIDs),
		AllowedGroupIDs: normalizeStringList(current.AllowedGroupIDs),
	}
	if strings.TrimSpace(current.Secret) != "" {
		settings.Secret = notificationSecretMask
	}
	return settings
}

func buildWeComBotConfig(request *weComBotNotificationSettings, current config.WeComBotConfig) (config.WeComBotConfig, error) {
	if request == nil {
		return current, nil
	}
	secret, err := resolveMaskedNotificationSecret(request.Secret, current.Secret, "企业微信长连接 Secret")
	if err != nil {
		return config.WeComBotConfig{}, err
	}
	webSocketURL := strings.TrimSpace(request.WebSocketURL)
	if webSocketURL == "" {
		webSocketURL = defaultWeComBotWebSocketURL
	}
	cfg := config.WeComBotConfig{
		Enabled: request.Enabled, BotID: strings.TrimSpace(request.BotID), Secret: secret,
		WebSocketURL: webSocketURL, AllowedUserIDs: normalizeStringList(request.AllowedUserIDs),
		AllowedGroupIDs: normalizeStringList(request.AllowedGroupIDs),
	}
	if err := validateWeComBotWebSocketURL(cfg.WebSocketURL); err != nil {
		return config.WeComBotConfig{}, err
	}
	if cfg.Enabled && (cfg.BotID == "" || cfg.Secret == "") {
		return config.WeComBotConfig{}, errors.New("企业微信长连接启用时必须填写 Bot ID 与 Secret")
	}
	return cfg, nil
}

func validateWeComBotWebSocketURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("企业微信长连接地址无效")
	}
	if parsed.Scheme == "wss" {
		return nil
	}
	if parsed.Scheme == "ws" && isLoopbackHost(parsed.Hostname()) {
		return nil
	}
	return errors.New("企业微信长连接地址必须使用 WSS；本机测试可使用 WS")
}

func normalizeStringList(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
