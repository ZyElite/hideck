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

type startWeixinQRRequest struct {
	BaseURL string `json:"base_url"`
}

type cancelWeixinQRRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

type weixinQRResponse struct {
	SessionID    string                `json:"session_id"`
	QRToken      string                `json:"qr_token,omitempty"`
	QRURL        string                `json:"qr_url,omitempty"`
	ExpiresAt    string                `json:"expires_at,omitempty"`
	Status       notify.WeixinQRStatus `json:"status"`
	Applied      bool                  `json:"applied,omitempty"`
	ApplyWarning string                `json:"apply_warning,omitempty"`
	Error        string                `json:"error,omitempty"`
	BotAccountID string                `json:"bot_account_id,omitempty"`
	BotUserID    string                `json:"bot_user_id,omitempty"`
	BaseURL      string                `json:"base_url,omitempty"`
}

func (s *Server) handleStartWeixinQR(c *gin.Context) {
	var request startWeixinQRRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "参数错误"})
			return
		}
	}
	baseURL := strings.TrimSpace(request.BaseURL)
	if baseURL == "" {
		s.notificationConfigMu.Lock()
		baseURL = s.fullCfg.Weixin.BaseURL
		s.notificationConfigMu.Unlock()
	}
	view, err := s.weixinQR.Start(c.Request.Context(), baseURL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, buildWeixinQRResponse(view))
}

func (s *Server) handleWeixinQRStatus(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Query("session_id"))
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "session_id 不能为空"})
		return
	}
	view, err := s.weixinQR.Status(c.Request.Context(), sessionID)
	if errors.Is(err, notify.ErrWeixinQRSessionNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"status": "error", "message": err.Error()})
		return
	}
	if view.Status == notify.WeixinQRConfirmed && !view.Applied {
		warning := s.applyWeixinQRCredentials(view.Credentials)
		_ = s.weixinQR.MarkApplied(sessionID, warning)
		view, _ = s.weixinQR.Status(c.Request.Context(), sessionID)
	}
	c.JSON(http.StatusOK, buildWeixinQRResponse(view))
}

func (s *Server) handleCancelWeixinQR(c *gin.Context) {
	var request cancelWeixinQRRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "session_id 不能为空"})
		return
	}
	if err := s.weixinQR.Cancel(request.SessionID); errors.Is(err, notify.ErrWeixinQRSessionNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) applyWeixinQRCredentials(credentials notify.WeixinQRCredentials) string {
	if s.notifyMgr == nil {
		return "通知管理器不可用，凭证尚未保存"
	}
	if err := s.notifyMgr.UpdateRuntimeState(func(state *notify.RuntimeState) error {
		state.Weixin.AccountID = credentials.AccountID
		state.Weixin.Token = credentials.Token
		state.Weixin.BaseURL = credentials.BaseURL
		state.Weixin.UserID = credentials.UserID
		return nil
	}); err != nil {
		return err.Error()
	}
	s.notificationConfigMu.Lock()
	defer s.notificationConfigMu.Unlock()
	nextConfig := *s.fullCfg
	nextConfig.Weixin.Enabled = true
	nextConfig.Weixin.BaseURL = credentials.BaseURL
	if err := config.UpdateNotificationInFile(s.configPath, notificationConfigsFrom(&nextConfig)); err != nil {
		return err.Error()
	}
	s.fullCfg.Weixin = nextConfig.Weixin
	if err := s.notifyMgr.UpdateConfig(s.fullCfg); err != nil {
		return "凭证已保存，但渠道启动失败: " + err.Error()
	}
	return ""
}

func buildWeixinQRResponse(view notify.WeixinQRView) weixinQRResponse {
	response := weixinQRResponse{
		SessionID: view.SessionID, QRToken: view.QRToken, QRURL: view.QRURL,
		ExpiresAt: view.ExpiresAt.Format(time.RFC3339), Status: view.Status,
		Applied: view.Applied, ApplyWarning: view.ApplyWarning, Error: view.Error,
	}
	if view.Status == notify.WeixinQRConfirmed {
		response.BotAccountID = view.Credentials.AccountID
		response.BotUserID = view.Credentials.UserID
		response.BaseURL = view.Credentials.BaseURL
	}
	return response
}
