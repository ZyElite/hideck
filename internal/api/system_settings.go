package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/pkg/logger"

	"github.com/gin-gonic/gin"
)

type systemSettingsPayload struct {
	OpenWRTDynamicInterfaces bool `json:"openwrt_dynamic_interfaces"`
}

func (s *Server) handleGetSystemSettings(c *gin.Context) {
	enabled := false
	if s.pool != nil {
		enabled = s.pool.OpenWRTDynamicInterfacesEnabled()
	} else if s.fullCfg != nil {
		enabled = s.fullCfg.System.OpenWRTDynamicInterfaces
	}
	c.JSON(http.StatusOK, systemSettingsPayload{OpenWRTDynamicInterfaces: enabled})
}

func (s *Server) handleUpdateSystemSettings(c *gin.Context) {
	var request systemSettingsPayload
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "参数错误"})
		return
	}
	if s.pool == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "设备池未初始化"})
		return
	}

	previous := s.pool.OpenWRTDynamicInterfacesEnabled()
	if err := s.pool.ConfigureOpenWRTDynamicInterfaces(c.Request.Context(), request.OpenWRTDynamicInterfaces); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"status": "error", "message": err.Error()})
		return
	}
	system := config.SystemConfig{OpenWRTDynamicInterfaces: request.OpenWRTDynamicInterfaces}
	if err := config.UpdateSystemInFile(s.configPath, system); err != nil {
		rollbackErr := rollbackOpenWRTSetting(s, previous)
		logger.Error("写入 OpenWrt 动态接口映射配置失败", "err", err, "rollback_err", rollbackErr)
		message := "写入配置文件失败: " + err.Error()
		if rollbackErr != nil {
			message += "; 运行时回滚失败: " + rollbackErr.Error()
		}
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": message})
		return
	}
	if s.fullCfg != nil {
		s.fullCfg.System = system
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "applied": true})
}

func rollbackOpenWRTSetting(s *Server, enabled bool) error {
	if s == nil || s.pool == nil {
		return errors.New("设备池未初始化")
	}
	return s.pool.ConfigureOpenWRTDynamicInterfaces(context.Background(), enabled)
}
