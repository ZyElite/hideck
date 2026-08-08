package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iniwex5/vohive/pkg/logger"
	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
)

const voWiFiCallSetupGrace = 45 * time.Second

type deviceVoWiFiCallRequest struct {
	Callee      string `json:"callee"`
	HoldSeconds int    `json:"hold_seconds"`
}

func (s *Server) handleDeviceVoWiFiCall(c *gin.Context) {
	deviceID := strings.TrimSpace(c.Param("device_id"))
	var req deviceVoWiFiCallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "code": "invalid_request", "message": err.Error()})
		return
	}
	req.Callee = strings.TrimSpace(req.Callee)
	holdSeconds, errMessage := validateVoWiFiCallRequest(deviceID, req)
	if errMessage != "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "code": "invalid_request", "message": errMessage})
		return
	}
	if !s.voiceAgentReady(deviceID) {
		c.JSON(http.StatusConflict, gin.H{"status": "error", "code": "vowifi_voice_not_ready", "message": "VoWiFi 语音未就绪"})
		return
	}

	traceID := requestID(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(holdSeconds)*time.Second+voWiFiCallSetupGrace)
	defer cancel()
	logger.Info("VoWiFi 外呼请求开始", "device", deviceID, "callee_suffix", maskedCallee(req.Callee), "hold_seconds", holdSeconds, "trace_id", traceID)
	result, err := s.voiceGW.SimulateCall(ctx, deviceID, voicehost.SimulateCallRequest{
		Callee: req.Callee, HoldSeconds: holdSeconds,
		OnConnected: func() { logger.Info("VoWiFi 外呼已接通并启动 RTP", "device", deviceID, "trace_id", traceID) },
	})
	if err != nil {
		logger.Error("VoWiFi 外呼失败", "device", deviceID, "err", err, "trace_id", traceID)
		c.JSON(http.StatusBadGateway, gin.H{"status": "error", "code": "vowifi_call_failed", "message": err.Error(), "trace_id": traceID})
		return
	}
	logger.Info("VoWiFi 外呼完成并已发送 BYE", "device", deviceID, "duration_ms", result.DurationMs, "trace_id", traceID)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "success": result.Success, "message": result.Message, "duration_ms": result.DurationMs, "trace_id": traceID})
}

func validateVoWiFiCallRequest(deviceID string, req deviceVoWiFiCallRequest) (int, string) {
	if deviceID == "" {
		return 0, "device_id 不能为空"
	}
	if req.Callee == "" {
		return 0, "callee 不能为空"
	}
	if req.HoldSeconds < 0 || req.HoldSeconds > voicehost.MaxSimulateCallHoldSeconds {
		return 0, "hold_seconds 必须在 0 到 60 之间"
	}
	if req.HoldSeconds == 0 {
		return voicehost.DefaultSimulateCallHoldSeconds, ""
	}
	return req.HoldSeconds, ""
}

func (s *Server) voiceAgentReady(deviceID string) bool {
	if s == nil || s.voiceGW == nil || s.voiceGW.GetAgent(deviceID) == nil {
		return false
	}
	ready, _ := s.voiceGW.DeviceStatus(deviceID)["ready"].(bool)
	return ready
}

func maskedCallee(callee string) string {
	if len(callee) <= 4 {
		return callee
	}
	return "***" + callee[len(callee)-4:]
}
