package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/internal/notify"
	"github.com/yibaiba/hideck/pkg/logger"
)

type cancelFeishuQRRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

type feishuQRResponse struct {
	SessionID    string                `json:"session_id"`
	QRURL        string                `json:"qr_url,omitempty"`
	OpenURL      string                `json:"open_url,omitempty"`
	ExpiresAt    string                `json:"expires_at,omitempty"`
	Status       notify.FeishuQRStatus `json:"status"`
	Applied      bool                  `json:"applied,omitempty"`
	ApplyWarning string                `json:"apply_warning,omitempty"`
	Error        string                `json:"error,omitempty"`
	AppID        string                `json:"app_id,omitempty"`
}

func (s *Server) handleStartFeishuQR(c *gin.Context) {
	view, err := s.feishuQR.Start(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"status": "error", "message": err.Error(), "manual_setup_available": true,
		})
		return
	}
	c.JSON(http.StatusOK, buildFeishuQRResponse(view))
}

func (s *Server) handleFeishuQRStatus(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Query("session_id"))
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "session_id 不能为空"})
		return
	}
	s.feishuQRMu.Lock()
	defer s.feishuQRMu.Unlock()
	view, err := s.feishuQR.Status(c.Request.Context(), sessionID)
	if errors.Is(err, notify.ErrFeishuQRSessionNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"status": "error", "message": err.Error()})
		return
	}
	if view.Status == notify.FeishuQRConfirmed && !view.Applied {
		warning := s.applyFeishuQRCredentials(view.Credentials)
		if err := s.feishuQR.MarkApplied(sessionID, warning); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"status": "error", "message": err.Error()})
			return
		}
		view, err = s.feishuQR.Status(c.Request.Context(), sessionID)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"status": "error", "message": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, buildFeishuQRResponse(view))
}

func (s *Server) handleCancelFeishuQR(c *gin.Context) {
	var request cancelFeishuQRRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "session_id 不能为空"})
		return
	}
	s.feishuQRMu.Lock()
	defer s.feishuQRMu.Unlock()
	if err := s.feishuQR.Cancel(request.SessionID); errors.Is(err, notify.ErrFeishuQRSessionNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) applyFeishuQRCredentials(credentials notify.FeishuQRCredentials) string {
	if strings.TrimSpace(credentials.AppID) == "" || strings.TrimSpace(credentials.AppSecret) == "" {
		return "飞书扫码凭证不完整，请手工填写 App ID 与 App Secret"
	}
	s.notificationConfigMu.Lock()
	defer s.notificationConfigMu.Unlock()
	nextConfig := *s.fullCfg
	nextConfig.Feishu.Enabled = true
	nextConfig.Feishu.AppID = credentials.AppID
	nextConfig.Feishu.AppSecret = credentials.AppSecret
	if err := config.UpdateNotificationInFile(s.configPath, notificationConfigsFrom(&nextConfig)); err != nil {
		return err.Error()
	}
	s.fullCfg.Feishu = nextConfig.Feishu
	if s.notifyMgr != nil {
		if openID := strings.TrimSpace(credentials.OpenID); openID != "" {
			if err := s.notifyMgr.UpdateRuntimeState(func(state *notify.RuntimeState) error {
				state.Feishu = notify.ApplyFeishuQRUserBinding(s.fullCfg.Feishu, state.Feishu, openID)
				return nil
			}); err != nil && !errors.Is(err, notify.ErrRuntimeStateStoreUnavailable) {
				return "凭证已保存，但飞书扫码用户绑定失败: " + err.Error()
			}
		}
		if err := s.notifyMgr.UpdateConfig(s.fullCfg); err != nil {
			return "凭证已保存，但渠道启动失败: " + err.Error()
		}
	}
	logger.Info("飞书扫码创建应用已保存", "app_id", credentials.AppID, "open_id", credentials.OpenID)
	return ""
}

func buildFeishuQRResponse(view notify.FeishuQRView) feishuQRResponse {
	response := feishuQRResponse{
		SessionID: view.SessionID, QRURL: view.QRURL, OpenURL: view.OpenURL,
		ExpiresAt: view.ExpiresAt.Format(time.RFC3339), Status: view.Status,
		Applied: view.Applied, ApplyWarning: view.ApplyWarning, Error: view.Error,
	}
	if view.Status == notify.FeishuQRConfirmed {
		response.AppID = view.Credentials.AppID
	}
	return response
}
