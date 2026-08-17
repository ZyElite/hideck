package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/db"
	"github.com/yibaiba/hideck/pkg/logger"
)

type enabledPatchRequest struct {
	Enabled      *bool   `json:"enabled"`
	Mode         *string `json:"mode"`          // "wifi" | "cellular"
	DataStrategy *string `json:"data_strategy"`  // "always" | "on_demand"
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
	s.pool.SetWorkerNetworkPolicy(deviceID, false, "", "")
	if err := s.pool.ApplyCurrentCardPolicy(deviceID, "network_disabled"); err != nil {
		logger.Debug("关闭网络后重投影卡策略失败", "device", deviceID, "err", err)
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
		// 落库：置 vowifi_enabled=true。若带 mode/data_strategy 则一并落库。
		mode := normalizePhoneMode(req.Mode)
		strategy := normalizeDataStrategy(req.DataStrategy)
		if _, _, err := s.patchCardPolicyForDevice(deviceID, func(p *db.CardPolicy) {
			p.VoWiFiEnabled = true
			if req.Mode != nil {
				p.PhoneMode = mode
			}
			if req.DataStrategy != nil {
				p.DataStrategy = strategy
			}
		}); err != nil {
			writeCardPolicyMutationError(c, err)
			return
		}
		// 同步 w.Config，使概览即时切到 VoWiFi 模式面板（EnableVoWiFi 不碰 Config）。
		w := s.pool.GetWorker(deviceID)
		prevMode, prevStrategy := "", ""
		if w != nil {
			prevMode = w.Config.PhoneMode
			if prevMode == "" {
				prevMode = "wifi"
			}
			prevStrategy = w.Config.DataStrategy
			if prevStrategy == "" {
				prevStrategy = "on_demand"
			}
			if req.Mode != nil {
				w.Config.PhoneMode = mode
			}
			if req.DataStrategy != nil {
				w.Config.DataStrategy = strategy
			}
		}
		s.pool.SetWorkerVoWiFiPolicy(deviceID, true)
		if w := s.pool.GetWorker(deviceID); w != nil && w.Config.PhoneMode == "cellular" && w.Config.DataStrategy != "always" {
			if err := s.pool.StopVoWiFiRuntimeForCellularIdle(deviceID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"status":  "error",
					"message": "切换蜂窝模式失败: " + err.Error(),
					"device":  deviceID,
				})
				return
			}
			message := "蜂窝模式已设置。请先打开「网络」，仅打电话时开 / 长时间开启才会驻网"
			if w.Config.NetworkEnabled {
				message = "蜂窝模式已设置，仅打电话时开：拨号才连数据"
			}
			c.JSON(http.StatusOK, gin.H{
				"status":  "ok",
				"message": message,
				"device":  deviceID,
			})
			return
		}
		modeChanged := req.Mode != nil && mode != prevMode
		strategyChanged := req.DataStrategy != nil && strategy != prevStrategy
		if s.pool.IsVoWiFiActive(deviceID) && (modeChanged || strategyChanged) {
			if err := s.pool.RestartVoWiFi(deviceID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"status":  "error",
					"message": "切换通话模式失败: " + err.Error(),
					"device":  deviceID,
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"status":  "ok",
				"message": "通话模式已切换",
				"device":  deviceID,
			})
			return
		}
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

func normalizePhoneMode(v *string) string {
	if v == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(*v)) {
	case "cellular":
		return "cellular"
	default:
		return "wifi"
	}
}

func normalizeDataStrategy(v *string) string {
	if v == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(*v)) {
	case "always":
		return "always"
	default:
		return "on_demand"
	}
}

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
