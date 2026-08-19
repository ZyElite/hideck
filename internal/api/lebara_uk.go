package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/db"
	"github.com/yibaiba/hideck/internal/device"
)

func (s *Server) classifyLebaraUKForDevice(ctx context.Context, deviceID string) (device.LebaraUKClass, error) {
	if s == nil || s.pool == nil {
		return device.LebaraUKClass{}, nil
	}
	return device.ClassifyWorkerLebaraUKForControl(ctx, s.pool.GetWorker(deviceID))
}

func (s *Server) classifyLebaraUKForICCID(ctx context.Context, iccid string) (device.LebaraUKClass, error) {
	if w := s.workerForICCID(iccid); w != nil {
		return device.ClassifyWorkerLebaraUKForControl(ctx, w)
	}
	return device.ClassifyLebaraUKForICCID(iccid, "")
}

func (s *Server) workerForICCID(iccid string) *device.Worker {
	if s == nil || s.pool == nil {
		return nil
	}
	iccid = db.CanonicalICCID(iccid)
	if iccid == "" {
		return nil
	}
	for _, w := range s.pool.GetAllWorkers() {
		if db.CanonicalICCID(w.CurrentICCID()) == iccid {
			return w
		}
	}
	return nil
}

func rejectLebaraUKRFUnlock(c *gin.Context, class device.LebaraUKClass, err error) bool {
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": fmt.Sprintf("识别 Lebara UK 射频策略失败: %v", err),
		})
		return true
	}
	if !class.IsLebara {
		return false
	}
	c.JSON(http.StatusConflict, gin.H{
		"status":  "error",
		"message": device.ErrLebaraUKRFLocked.Error(),
		"rf_lock": class.RFLock(),
	})
	return true
}

func writeLebaraUKRFLockError(c *gin.Context, err error) bool {
	if err == nil || !device.IsLebaraUKPolicyError(err) {
		return false
	}
	status := http.StatusConflict
	if errors.Is(err, device.ErrLebaraUKRFLocked) {
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"status": "error", "message": err.Error(), "rf_lock": device.RFLockLebaraUKNextGen})
	return true
}
