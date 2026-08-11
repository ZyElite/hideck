package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/modem"
)

const backendSwitchATTimeout = 5 * time.Second

func (s *backendSwitchService) applyHardwareMode(atPort, target string) (int, bool, error) {
	if strings.TrimSpace(atPort) == "" {
		return -1, false, fmt.Errorf("当前设备没有可验证的 AT 端口")
	}
	session, err := s.openAT(atPort)
	if err != nil {
		return -1, false, fmt.Errorf("打开 AT 端口 %s 失败: %w", atPort, err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = session.Close()
		}
	}()

	queryResponse, err := executeBackendSwitchAT(session, "AT+QCFG=\"usbnet\"?", backendSwitchATTimeout)
	if err != nil {
		return -1, false, err
	}
	currentMode, err := modem.ParseUSBNetMode(queryResponse)
	if err != nil {
		return -1, false, err
	}
	targetMode := usbNetModeForBackend(target)
	if currentMode != 0 && currentMode != 2 {
		return currentMode, false, fmt.Errorf("设备报告未知 USBNET 模式 %d，拒绝自动猜测", currentMode)
	}

	changed := currentMode != targetMode
	if changed {
		command := fmt.Sprintf("AT+QCFG=\"usbnet\",%d", targetMode)
		if _, err := executeBackendSwitchAT(session, command, backendSwitchATTimeout); err != nil {
			return currentMode, false, err
		}
		if _, err := executeBackendSwitchAT(session, "AT+CFUN=1,1", backendSwitchATTimeout); err != nil {
			return currentMode, true, err
		}
	}
	if err := session.Close(); err != nil {
		return currentMode, changed, fmt.Errorf("关闭 AT 端口失败: %w", err)
	}
	closed = true
	return targetMode, changed, nil
}

func executeBackendSwitchAT(session manualATSession, command string, timeout time.Duration) (string, error) {
	response, err := session.Execute(command, timeout)
	if err != nil {
		return response, fmt.Errorf("执行 %s 失败: %w", command, err)
	}
	upper := strings.ToUpper(response)
	if strings.Contains(upper, "ERROR") || !strings.Contains(upper, "OK") {
		return response, fmt.Errorf("执行 %s 未得到 OK: %q", command, strings.TrimSpace(response))
	}
	return response, nil
}
