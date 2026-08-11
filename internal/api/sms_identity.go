package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/db"
	"github.com/iniwex5/vohive/internal/device"
)

type smsDeviceInfo struct {
	id   string
	name string
}

// resolveSMSICCID 将 device_id 或 imsi 查询参数解析为唯一 ICCID。
func (s *Server) resolveSMSICCID(deviceID, imsi string) (string, int, string) {
	deviceID = strings.TrimSpace(deviceID)
	imsi = strings.TrimSpace(imsi)
	if deviceID == "" || deviceID == "all" {
		if imsi == "" {
			return "", http.StatusBadRequest, "缺少 imsi 参数（device_id=all 时必须指定）"
		}
		iccid, err := db.ResolveICCIDForIMSI(imsi)
		if err != nil {
			status, message := smsIdentityHTTPError(err)
			return "", status, message
		}
		return iccid, 0, ""
	}
	if s.pool == nil {
		return "", http.StatusServiceUnavailable, "设备服务未就绪"
	}
	iccid, err := s.pool.ResolveSMSICCID(deviceID)
	if err != nil {
		status, message := smsIdentityHTTPError(err)
		return "", status, message
	}
	return iccid, 0, ""
}

func smsIdentityHTTPError(err error) (int, string) {
	switch {
	case errors.Is(err, device.ErrSMSIdentityConflict), errors.Is(err, db.ErrSMSIdentityConflict):
		return http.StatusConflict, "设备短信身份冲突: " + err.Error()
	case errors.Is(err, device.ErrSMSIdentityTransitioning):
		return http.StatusConflict, "设备 SIM 身份正在切换，请稍后重试"
	case errors.Is(err, device.ErrSMSIdentityUnknown), errors.Is(err, db.ErrSMSIdentityUnknown):
		return http.StatusBadRequest, "该设备短信身份未就绪"
	default:
		return http.StatusInternalServerError, "解析设备短信身份失败: " + err.Error()
	}
}

func (s *Server) smsDeviceInfoByICCID() map[string]smsDeviceInfo {
	result := make(map[string]smsDeviceInfo)
	if s.pool == nil {
		return result
	}
	configured := config.ListDevices()
	seen := make(map[string]struct{}, len(configured))
	for _, cfg := range configured {
		seen[cfg.ID] = struct{}{}
		s.addSMSDeviceInfo(result, cfg.ID, cfg.Name)
	}
	for _, worker := range s.pool.GetAllWorkers() {
		if worker == nil {
			continue
		}
		if _, ok := seen[worker.ID]; ok {
			continue
		}
		s.addSMSDeviceInfo(result, worker.ID, worker.Config.Name)
	}
	return result
}

func (s *Server) addSMSDeviceInfo(result map[string]smsDeviceInfo, deviceID, name string) {
	iccid, err := s.pool.ResolveSMSICCID(deviceID)
	if err != nil || iccid == "" {
		return
	}
	if strings.TrimSpace(name) == "" {
		name = deviceID
	}
	result[iccid] = smsDeviceInfo{id: deviceID, name: name}
}
