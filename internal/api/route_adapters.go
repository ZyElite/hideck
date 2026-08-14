package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/db"
)

type enabledPatchRequest struct {
	Enabled *bool `json:"enabled"`
}

type networkPatchRequest struct {
	Enabled   *bool   `json:"enabled"`
	IPVersion *string `json:"ip_version"`
	APN       *string `json:"apn"`
}

func (s *Server) handleDeviceNetworkPatch(c *gin.Context) {
	var req networkPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "enabled 为必填项"})
		return
	}

	deviceID := deviceIDParam(c)

	if *req.Enabled {
		ipVersion, err := normalizedOptionalIPVersion(req.IPVersion)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
			return
		}
		apn := trimmedOptionalString(req.APN)
		effectiveIPVersion := ""
		effectiveAPN := ""
		_, _, err = s.patchCardPolicyForDevice(deviceID, func(p *db.CardPolicy) {
			p.NetworkEnabled = true
			if ipVersion != "" {
				p.IPVersion = ipVersion
			}
			if req.APN != nil {
				p.APN = apn
			}
			effectiveIPVersion = p.IPVersion
			effectiveAPN = p.APN
		})
		if err != nil {
			writeCardPolicyMutationError(c, err)
			return
		}
		s.pool.SetWorkerNetworkPolicy(deviceID, true, effectiveIPVersion, effectiveAPN)
		s.handleDeviceMgmtStartNetwork(c)
		return
	}

	if _, _, err := s.patchCardPolicyForDevice(deviceID, func(p *db.CardPolicy) {
		p.NetworkEnabled = false
	}); err != nil {
		writeCardPolicyMutationError(c, err)
		return
	}
	s.handleDeviceMgmtStopNetwork(c)
}

func (s *Server) handleDeviceVoWiFiPatch(c *gin.Context) {
	var req enabledPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "enabled 为必填项"})
		return
	}

	deviceID := deviceIDParam(c)

	if *req.Enabled {
		// 落库：仅置 vowifi_enabled=true。不碰 airplane_enabled——它是用户的纯飞行
		// 意图，作为关闭 VoWiFi 后的回退依据；VoWiFi 接管射频由运行时投影派生。
		if _, _, err := s.patchCardPolicyForDevice(deviceID, vowifiEnablePolicyMutation); err != nil {
			writeCardPolicyMutationError(c, err)
			return
		}
		// 同步 w.Config，使概览即时切到 VoWiFi 模式面板（EnableVoWiFi 不碰 Config）。
		s.pool.SetWorkerVoWiFiPolicy(deviceID, true)
		s.handleVoWiFiEnable(c)
		return
	}

	// 落库：仅清 vowifi_enabled=false，保留 airplane_enabled（用户飞行意图）。
	// 关闭 VoWiFi 后 DisableVoWiFi 会按当前卡策略重投影：之前是飞行则回飞行，否则回在线。
	if _, _, err := s.patchCardPolicyForDevice(deviceID, vowifiDisablePolicyMutation); err != nil {
		writeCardPolicyMutationError(c, err)
		return
	}
	s.pool.SetWorkerVoWiFiPolicy(deviceID, false)
	s.handleVoWiFiDisable(c)
}

// vowifiEnablePolicyMutation 开 VoWiFi 的落库副作用：只置 vowifi，飞行意图保持不变。
func vowifiEnablePolicyMutation(p *db.CardPolicy) { p.VoWiFiEnabled = true }

// vowifiDisablePolicyMutation 关 VoWiFi 的落库副作用：只清 vowifi，保留用户飞行意图以便回退。
func vowifiDisablePolicyMutation(p *db.CardPolicy) { p.VoWiFiEnabled = false }

func normalizedOptionalIPVersion(value *string) (string, error) {
	if value == nil {
		return "", nil
	}
	return normalizeCardPolicyIPVersion(*value)
}

func trimmedOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
