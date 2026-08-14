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

type cancelQQQRRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

type qqQRResponse struct {
	SessionID    string            `json:"session_id"`
	TaskID       string            `json:"task_id,omitempty"`
	QRURL        string            `json:"qr_url,omitempty"`
	ExpiresAt    string            `json:"expires_at,omitempty"`
	Status       notify.QQQRStatus `json:"status"`
	Applied      bool              `json:"applied,omitempty"`
	ApplyWarning string            `json:"apply_warning,omitempty"`
	Error        string            `json:"error,omitempty"`
	AppID        string            `json:"app_id,omitempty"`
	UserOpenID   string            `json:"user_openid,omitempty"`
}

func (s *Server) handleStartQQQR(c *gin.Context) {
	view, err := s.qqQR.Start(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"status": "error", "message": err.Error(), "manual_setup_available": true,
		})
		return
	}
	c.JSON(http.StatusOK, buildQQQRResponse(view))
}

func (s *Server) handleQQQRStatus(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Query("session_id"))
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "session_id 不能为空"})
		return
	}
	view, err := s.qqQR.Status(c.Request.Context(), sessionID)
	if errors.Is(err, notify.ErrQQQRSessionNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"status": "error", "message": err.Error()})
		return
	}
	if view.Status == notify.QQQRConfirmed && !view.Applied {
		warning := s.applyQQQRCredentials(view.Credentials)
		_ = s.qqQR.MarkApplied(sessionID, warning)
		view, _ = s.qqQR.Status(c.Request.Context(), sessionID)
	}
	c.JSON(http.StatusOK, buildQQQRResponse(view))
}

func (s *Server) handleCancelQQQR(c *gin.Context) {
	var request cancelQQQRRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "session_id 不能为空"})
		return
	}
	if err := s.qqQR.Cancel(request.SessionID); errors.Is(err, notify.ErrQQQRSessionNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) applyQQQRCredentials(credentials notify.QQQRCredentials) string {
	if strings.TrimSpace(credentials.AppID) == "" || strings.TrimSpace(credentials.ClientSecret) == "" ||
		strings.TrimSpace(credentials.UserOpenID) == "" {
		return "QQ 扫码凭证不完整，请手工填写 App ID、Secret 和私聊 OpenID"
	}
	if s.notifyMgr == nil {
		return "通知管理器不可用，QQ 扫码凭证尚未保存"
	}
	if err := s.notifyMgr.UpdateRuntimeState(func(state *notify.RuntimeState) error {
		state.QQ.AdminOpenID = credentials.UserOpenID
		state.QQ.AllowedDirect = appendUniqueString(state.QQ.AllowedDirect, credentials.UserOpenID)
		state.QQ.DefaultTarget = credentials.UserOpenID
		return nil
	}); err != nil {
		return err.Error()
	}
	nextConfig := *s.fullCfg
	nextConfig.QQ.Enabled = true
	nextConfig.QQ.AppID = credentials.AppID
	nextConfig.QQ.AppSecret = credentials.ClientSecret
	nextConfig.QQ.DirectIDs = appendCommaSeparatedID(nextConfig.QQ.DirectIDs, credentials.UserOpenID)
	if err := config.UpdateNotificationInFile(s.configPath, notificationConfigsFrom(&nextConfig)); err != nil {
		return err.Error()
	}
	s.fullCfg.QQ = nextConfig.QQ
	if err := s.notifyMgr.UpdateConfig(s.fullCfg); err != nil {
		return "凭证已保存，但渠道启动失败: " + err.Error()
	}
	return ""
}

func buildQQQRResponse(view notify.QQQRView) qqQRResponse {
	response := qqQRResponse{
		SessionID: view.SessionID, TaskID: view.TaskID, QRURL: view.QRURL,
		ExpiresAt: view.ExpiresAt.Format(time.RFC3339), Status: view.Status,
		Applied: view.Applied, ApplyWarning: view.ApplyWarning, Error: view.Error,
	}
	if view.Status == notify.QQQRConfirmed {
		response.AppID = view.Credentials.AppID
		response.UserOpenID = view.Credentials.UserOpenID
	}
	return response
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	for _, existing := range values {
		if strings.TrimSpace(existing) == value {
			return values
		}
	}
	return append(values, value)
}

func appendCommaSeparatedID(existing, value string) string {
	values := strings.Split(existing, ",")
	values = appendUniqueString(values, value)
	result := make([]string, 0, len(values))
	for _, item := range values {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return strings.Join(result, ",")
}
