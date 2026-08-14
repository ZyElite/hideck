package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/internal/notify"
)

type cancelWeComQRRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

type weComQRResponse struct {
	SessionID    string               `json:"session_id"`
	QRURL        string               `json:"qr_url,omitempty"`
	OpenURL      string               `json:"open_url,omitempty"`
	ExpiresAt    string               `json:"expires_at,omitempty"`
	Status       notify.WeComQRStatus `json:"status"`
	Applied      bool                 `json:"applied,omitempty"`
	ApplyWarning string               `json:"apply_warning,omitempty"`
	Error        string               `json:"error,omitempty"`
	BotID        string               `json:"bot_id,omitempty"`
}

func (s *Server) handleStartWeComQR(c *gin.Context) {
	view, err := s.wecomQR.Start(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"status": "error", "message": err.Error(), "manual_setup_available": true,
		})
		return
	}
	c.JSON(http.StatusOK, buildWeComQRResponse(view))
}

func (s *Server) handleWeComQRStatus(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Query("session_id"))
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "session_id 不能为空"})
		return
	}
	view, err := s.wecomQR.Status(c.Request.Context(), sessionID)
	if errors.Is(err, notify.ErrWeComQRSessionNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"status": "error", "message": err.Error()})
		return
	}
	if view.Status == notify.WeComQRConfirmed && !view.Applied {
		warning := s.applyWeComQRCredentials(view.Credentials)
		_ = s.wecomQR.MarkApplied(sessionID, warning)
		view, _ = s.wecomQR.Status(c.Request.Context(), sessionID)
	}
	c.JSON(http.StatusOK, buildWeComQRResponse(view))
}

func (s *Server) handleCancelWeComQR(c *gin.Context) {
	var request cancelWeComQRRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "session_id 不能为空"})
		return
	}
	if err := s.wecomQR.Cancel(request.SessionID); errors.Is(err, notify.ErrWeComQRSessionNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) applyWeComQRCredentials(credentials notify.WeComQRCredentials) string {
	if strings.TrimSpace(credentials.BotID) == "" || strings.TrimSpace(credentials.Secret) == "" {
		return "企业微信扫码凭证不完整，请手工填写 Bot ID 与 Secret"
	}
	nextConfig := *s.fullCfg
	nextConfig.WeComBot.Enabled = true
	nextConfig.WeComBot.BotID = credentials.BotID
	nextConfig.WeComBot.Secret = credentials.Secret
	if strings.TrimSpace(nextConfig.WeComBot.WebSocketURL) == "" {
		nextConfig.WeComBot.WebSocketURL = defaultWeComBotWebSocketURL
	}
	if err := config.UpdateNotificationInFile(s.configPath, notificationConfigsFrom(&nextConfig)); err != nil {
		return err.Error()
	}
	s.fullCfg.WeComBot = nextConfig.WeComBot
	if s.notifyMgr != nil {
		if err := s.notifyMgr.UpdateConfig(s.fullCfg); err != nil {
			return "凭证已保存，但渠道启动失败: " + err.Error()
		}
	}
	return ""
}

func buildWeComQRResponse(view notify.WeComQRView) weComQRResponse {
	response := weComQRResponse{
		SessionID: view.SessionID, QRURL: view.QRURL, OpenURL: view.OpenURL,
		ExpiresAt: view.ExpiresAt.Format(time.RFC3339), Status: view.Status,
		Applied: view.Applied, ApplyWarning: view.ApplyWarning, Error: view.Error,
	}
	if view.Status == notify.WeComQRConfirmed {
		response.BotID = view.Credentials.BotID
	}
	return response
}
