package api

import (
	"errors"
	"net/url"
	"strings"

	"github.com/yibaiba/hideck/internal/config"
)

const defaultWeixinBaseURL = "https://ilinkai.weixin.qq.com"

type weixinNotificationSettings struct {
	Enabled         bool     `json:"enabled"`
	BaseURL         string   `json:"base_url"`
	AllowedUserIDs  []string `json:"allowed_user_ids"`
	AllowedGroupIDs []string `json:"allowed_group_ids"`
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
