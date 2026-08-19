package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/internal/db"
	"github.com/yibaiba/hideck/internal/device"
)

var errCardPolicyIdentityUnavailable = errors.New("SIM 身份未就绪，无法保存卡策略")

type cardPolicyStore interface {
	Get(string) (db.CardPolicy, error)
	Resolve(string) (db.CardPolicy, error)
	Upsert(db.CardPolicy) error
}

type databaseCardPolicyStore struct{}

func (databaseCardPolicyStore) Get(iccid string) (db.CardPolicy, error) {
	return db.GetCardPolicy(iccid)
}

func (databaseCardPolicyStore) Resolve(iccid string) (db.CardPolicy, error) {
	return db.ResolveCardPolicy(iccid)
}

func (databaseCardPolicyStore) Upsert(policy db.CardPolicy) error {
	return db.UpsertCardPolicy(policy)
}

func (s *Server) cardPolicyStore() cardPolicyStore {
	if s.cardPolicies != nil {
		return s.cardPolicies
	}
	return databaseCardPolicyStore{}
}

// patchCardPolicyForDevice 解析设备当前 ICCID，对 card_policies 行执行原地修改并落库。
// mutate 在 resolve 后的副本上改字段（source 会被强制为 "user"）。
func (s *Server) patchCardPolicyForDevice(deviceID string, mutate func(*db.CardPolicy)) (iccid string, applied bool, err error) {
	worker := s.pool.GetWorker(deviceID)
	if worker == nil {
		return "", false, fmt.Errorf("设备未找到")
	}
	iccid = worker.CurrentICCID()
	if iccid == "" {
		return "", false, errCardPolicyIdentityUnavailable
	}
	p, err := s.cardPolicyStore().Resolve(iccid)
	if err != nil {
		return iccid, false, fmt.Errorf("获取卡策略失败: %w", err)
	}
	mutate(&p)
	p.Source = "user"
	db.NormalizeCardPolicy(&p)
	if err := s.cardPolicyStore().Upsert(p); err != nil {
		return iccid, false, fmt.Errorf("保存卡策略失败: %w", err)
	}
	return iccid, true, nil
}

func (s *Server) handleGetCardPolicy(c *gin.Context) {
	iccid := c.Param("iccid")
	pol, err := s.cardPolicyStore().Get(iccid)
	if errors.Is(err, db.ErrCardPolicyNotFound) {
		// 未建档则返回默认模板（不落库，读端点保持只读语义）
		c.JSON(http.StatusOK, db.DefaultCardPolicy(iccid))
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, pol)
}

func (s *Server) handleListCardPolicies(c *gin.Context) {
	var out []db.CardPolicy
	if db.DB != nil {
		if err := db.DB.Order("updated_at desc").Find(&out).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"policies": out})
}

func (s *Server) handlePutCardPolicy(c *gin.Context) {
	iccid := c.Param("iccid")
	var req struct {
		NetworkEnabled  *bool   `json:"network_enabled"`
		VoWiFiEnabled   *bool   `json:"vowifi_enabled"`
		AirplaneEnabled *bool   `json:"airplane_enabled"`
		IPVersion       *string `json:"ip_version"`
		APN             *string `json:"apn"`
		PhoneMode       *string `json:"phone_mode"`
		DataStrategy    *string `json:"data_strategy"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	pol, err := s.cardPolicyStore().Get(iccid)
	if errors.Is(err, db.ErrCardPolicyNotFound) {
		pol = db.DefaultCardPolicy(iccid)
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取卡策略失败: " + err.Error()})
		return
	}

	class := s.classifyLebaraUKForICCID(iccid)
	if class.IsLebara {
		unlocksRadio := req.NetworkEnabled != nil && *req.NetworkEnabled
		if req.AirplaneEnabled != nil && !*req.AirplaneEnabled {
			unlocksRadio = true
		}
		if normalizePhoneMode(req.PhoneMode) == "cellular" {
			unlocksRadio = true
		}
		if unlocksRadio {
			c.JSON(http.StatusConflict, gin.H{
				"error":   device.ErrLebaraUKRFLocked.Error(),
				"rf_lock": class.RFLock(),
			})
			return
		}
	}

	if req.NetworkEnabled != nil {
		pol.NetworkEnabled = *req.NetworkEnabled
	}
	if req.VoWiFiEnabled != nil {
		pol.VoWiFiEnabled = *req.VoWiFiEnabled
	}
	if req.AirplaneEnabled != nil {
		pol.AirplaneEnabled = *req.AirplaneEnabled
	}
	if req.IPVersion != nil {
		normalized, err := normalizeCardPolicyIPVersion(*req.IPVersion)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		pol.IPVersion = normalized
	}
	if req.APN != nil {
		pol.APN = strings.TrimSpace(*req.APN)
	}
	if req.PhoneMode != nil {
		pol.PhoneMode = normalizePhoneMode(req.PhoneMode)
	}
	if req.DataStrategy != nil {
		pol.DataStrategy = normalizeDataStrategy(req.DataStrategy)
	}
	pol.Source = "user"

	if err := s.cardPolicyStore().Upsert(pol); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, pol)
}

func normalizeCardPolicyIPVersion(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	v4, v6, err := config.ResolveIPFamily(value)
	if err != nil {
		return "", err
	}
	switch {
	case v4 && v6:
		return "v4v6", nil
	case v6:
		return "v6", nil
	default:
		return "v4", nil
	}
}

func writeCardPolicyMutationError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, errCardPolicyIdentityUnavailable) {
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"status": "error", "message": err.Error()})
}
