package config

import (
	"fmt"
	"os"
	"path/filepath"

	yaml "go.yaml.in/yaml/v3"
)

func UpdateNotificationInFile(path string, notifications NotificationConfigs) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	root := make(map[string]any)
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}

	root["telegram"] = map[string]any{
		"enabled":   notifications.Telegram.Enabled,
		"bot_token": notifications.Telegram.BotToken,
		"chat_id":   notifications.Telegram.ChatID,
		"admin_id":  notifications.Telegram.AdminID,
		"base_url":  notifications.Telegram.BaseURL,
		"proxy":     notifications.Telegram.Proxy,
	}

	root["feishu"] = map[string]any{
		"enabled":    notifications.Feishu.Enabled,
		"app_id":     notifications.Feishu.AppID,
		"app_secret": notifications.Feishu.AppSecret,
		"chat_ids":   notifications.Feishu.ChatIDs,
	}

	root["qq"] = map[string]any{
		"enabled":    notifications.QQ.Enabled,
		"app_id":     notifications.QQ.AppID,
		"app_secret": notifications.QQ.AppSecret,
		"group_ids":  notifications.QQ.GroupIDs,
		"direct_ids": notifications.QQ.DirectIDs,
	}

	root["weixin"] = map[string]any{
		"enabled":           notifications.Weixin.Enabled,
		"base_url":          notifications.Weixin.BaseURL,
		"cdn_base_url":      notifications.Weixin.CDNBaseURL,
		"allowed_user_ids":  notifications.Weixin.AllowedUserIDs,
		"allowed_group_ids": notifications.Weixin.AllowedGroupIDs,
	}

	root["wecom_bot"] = map[string]any{
		"enabled":           notifications.WeComBot.Enabled,
		"bot_id":            notifications.WeComBot.BotID,
		"secret":            notifications.WeComBot.Secret,
		"websocket_url":     notifications.WeComBot.WebSocketURL,
		"allowed_user_ids":  notifications.WeComBot.AllowedUserIDs,
		"allowed_group_ids": notifications.WeComBot.AllowedGroupIDs,
	}

	root["webhook"] = map[string]any{
		"enabled":       notifications.Webhook.Enabled,
		"urls":          notifications.Webhook.URLs,
		"secret":        notifications.Webhook.Secret,
		"timeout_ms":    notifications.Webhook.TimeoutMs,
		"retry_max":     notifications.Webhook.RetryMax,
		"text_template": notifications.Webhook.TextTemplate,
		"headers":       notifications.Webhook.Headers,
	}

	root["bark"] = map[string]any{
		"enabled": notifications.Bark.Enabled,
		"urls":    notifications.Bark.URLs,
		"group":   notifications.Bark.Group,
		"icon":    notifications.Bark.Icon,
		"level":   notifications.Bark.Level,
	}

	root["email"] = map[string]any{
		"enabled":      notifications.Email.Enabled,
		"smtp_host":    notifications.Email.SMTPHost,
		"smtp_port":    notifications.Email.SMTPPort,
		"username":     notifications.Email.Username,
		"password":     notifications.Email.Password,
		"from_address": notifications.Email.FromAddress,
		"to_addresses": notifications.Email.ToAddresses,
	}

	root["pushplus"] = map[string]any{
		"enabled": notifications.Pushplus.Enabled,
		"token":   notifications.Pushplus.Token,
		"topic":   notifications.Pushplus.Topic,
		"channel": notifications.Pushplus.Channel,
	}

	root["wecom"] = map[string]any{
		"enabled":          notifications.WeCom.Enabled,
		"urls":             notifications.WeCom.URLs,
		"payload_template": notifications.WeCom.PayloadTemplate,
	}

	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("序列化配置文件失败: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return fmt.Errorf("写入临时配置文件失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("替换配置文件失败: %w", err)
	}
	return nil
}

// UpdateWebCredentialsInFile 更新配置文件中的 Web 凭证（用户名和密码）
func UpdateWebCredentialsInFile(path string, username, password string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	root := make(map[string]any)
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 更新 web 节点
	root["web"] = map[string]any{
		"username": username,
		"password": password,
	}

	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("序列化配置文件失败: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return fmt.Errorf("写入临时配置文件失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("替换配置文件失败: %w", err)
	}
	return nil
}
