package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/internal/device"
)

type deviceBackendSwitcherStub struct {
	request backendSwitchRequest
	calls   int
	result  backendSwitchResult
	err     error
}

func (s *deviceBackendSwitcherStub) Switch(
	_ context.Context,
	req backendSwitchRequest,
) (backendSwitchResult, error) {
	s.calls++
	s.request = req
	return s.result, s.err
}

func TestSetUSBNetModeRoutesThroughTransactionalSwitcher(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := writeDeviceMgmtLimitConfig(t, `
devices:
  - id: wwan1
    modem_imei: "866069053342612"
    device_backend: qmi
`)
	if err := config.InitGlobalManager(path); err != nil {
		t.Fatalf("InitGlobalManager() error = %v", err)
	}
	switcher := &deviceBackendSwitcherStub{result: backendSwitchResult{
		DeviceID:         "wwan1",
		TargetBackend:    "mbim",
		Persisted:        true,
		WorkerStarted:    true,
		HardwareVerified: true,
	}}
	server := &Server{configPath: path, backendSwitch: switcher}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "device_id", Value: "wwan1"}}
	ctx.Request = httptest.NewRequest(
		http.MethodPatch,
		"/devices/wwan1/usbnet-mode",
		strings.NewReader(`{"mode":2}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	server.handleDeviceMgmtSetUSBNetMode(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if switcher.calls != 1 || switcher.request.Target != "mbim" {
		t.Fatalf("switcher calls=%d request=%+v", switcher.calls, switcher.request)
	}
	if switcher.request.Current.DeviceBackend != "qmi" || switcher.request.Desired.DeviceBackend != "mbim" {
		t.Fatalf("switch request=%+v", switcher.request)
	}
}

func TestSetUSBNetModeRejectsUnmanagedMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	switcher := &deviceBackendSwitcherStub{}
	server := &Server{backendSwitch: switcher}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "device_id", Value: "wwan1"}}
	ctx.Request = httptest.NewRequest(
		http.MethodPatch,
		"/devices/wwan1/usbnet-mode",
		strings.NewReader(`{"mode":1}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	server.handleDeviceMgmtSetUSBNetMode(ctx)

	if recorder.Code != http.StatusBadRequest || switcher.calls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, switcher.calls, recorder.Body.String())
	}
}

func TestUpdateDeviceBackendChangeRoutesThroughTransactionalSwitcher(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initPolicyTestDB(t)
	path := writeDeviceMgmtLimitConfig(t, `
devices:
  - id: wwan1
    name: old
    modem_imei: "866069053342612"
    device_backend: qmi
`)
	if err := config.InitGlobalManager(path); err != nil {
		t.Fatalf("InitGlobalManager() error = %v", err)
	}
	pool := device.NewPool(&config.Config{})
	t.Cleanup(func() { _ = pool.Shutdown() })
	switcher := &deviceBackendSwitcherStub{result: backendSwitchResult{
		DeviceID:         "wwan1",
		TargetBackend:    "mbim",
		HardwareVerified: true,
		Persisted:        true,
		WorkerStarted:    true,
	}}
	server := &Server{pool: pool, configPath: path, backendSwitch: switcher}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "device_id", Value: "wwan1"}}
	ctx.Request = httptest.NewRequest(
		http.MethodPut,
		"/devices/wwan1",
		strings.NewReader(`{"config":{"id":"wwan1","name":"new","modem_imei":"866069053342612","device_backend":"mbim"}}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	server.handleDeviceMgmtUpdateDevice(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if switcher.calls != 1 || switcher.request.Target != "mbim" || switcher.request.Desired.Name != "new" {
		t.Fatalf("switcher calls=%d request=%+v", switcher.calls, switcher.request)
	}
}
