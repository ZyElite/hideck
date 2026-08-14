package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/notify"
)

const notificationSecretMask = "********"

type weComNotificationSettings struct {
	Enabled         bool     `json:"enabled"`
	URLs            []string `json:"urls"`
	PayloadTemplate string   `json:"payload_template"`
}

type testWeComResponse struct {
	OK          bool   `json:"ok"`
	Message     string `json:"message"`
	FailedCount int    `json:"failed_count,omitempty"`
}

func maskedWeComURLs(urls []string) []string {
	masked := make([]string, len(urls))
	for index := range masked {
		masked[index] = notificationSecretMask
	}
	return masked
}

func resolveWeComURLs(incoming, current []string) ([]string, error) {
	resolved := make([]string, 0, len(incoming))
	for index, rawURL := range incoming {
		value := strings.TrimSpace(rawURL)
		if value == "" {
			continue
		}
		if value != notificationSecretMask {
			resolved = append(resolved, value)
			continue
		}
		if index >= len(current) || strings.TrimSpace(current[index]) == "" {
			return nil, errors.New("企业微信 Webhook URL 脱敏值没有可保留的原配置")
		}
		resolved = append(resolved, current[index])
	}
	return resolved, nil
}

func buildWeComConfig(request *weComNotificationSettings, current config.WeComConfig) (config.WeComConfig, error) {
	if request == nil {
		return current, nil
	}
	urls, err := resolveWeComURLs(request.URLs, current.URLs)
	if err != nil {
		return config.WeComConfig{}, err
	}
	cfg := config.WeComConfig{
		Enabled:         request.Enabled,
		URLs:            urls,
		PayloadTemplate: request.PayloadTemplate,
	}
	if strings.TrimSpace(cfg.PayloadTemplate) == "" {
		return config.WeComConfig{}, errors.New("企业微信 JSON 请求体模板不能为空")
	}
	if err := notify.ValidateWeComPayloadTemplate(cfg.PayloadTemplate); err != nil {
		return config.WeComConfig{}, err
	}
	if !cfg.Enabled && len(cfg.URLs) == 0 {
		return cfg, nil
	}
	if err := notify.ValidateWeComConfig(cfg); err != nil {
		return config.WeComConfig{}, err
	}
	return cfg, nil
}

func (s *Server) handleTestWeComNotification(c *gin.Context) {
	var request weComNotificationSettings
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数错误"})
		return
	}
	if !request.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先启用企业微信消息推送后再测试"})
		return
	}
	cfg, err := buildWeComConfig(&request, s.fullCfg.WeCom)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	channel, err := notify.NewWeComChannel(cfg)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	defer channel.Close()

	result, sendErr := channel.SendWithContextDetailed(notify.NotificationContext{
		Event:      "wecom_test",
		Text:       "这是一条来自 VoHive 的企业微信测试通知",
		DeviceID:   "test_device_001",
		DeviceName: "测试设备",
		Timestamp:  time.Now(),
	})
	if sendErr != nil {
		c.JSON(http.StatusOK, testWeComResponse{
			OK:          false,
			Message:     "企业微信测试通知发送失败: " + sendErr.Error(),
			FailedCount: result.FailedCount,
		})
		return
	}
	c.JSON(http.StatusOK, testWeComResponse{OK: true, Message: "企业微信测试通知已发送"})
}
