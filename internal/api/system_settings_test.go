package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/internal/device"

	"github.com/gin-gonic/gin"
)

type settingsMapperStub struct {
	enabled     bool
	validateErr error
}

func (s *settingsMapperStub) Enabled() bool     { return s.enabled }
func (s *settingsMapperStub) SetEnabled(v bool) { s.enabled = v }
func (s *settingsMapperStub) Validate() error   { return s.validateErr }
func (s *settingsMapperStub) Add(context.Context, string, string) error {
	return nil
}
func (s *settingsMapperStub) Remove(context.Context, string) error { return nil }

func TestSystemSettingsHandlerPersistsAndAppliesOpenWRTMapping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := writeSystemSettingsFixture(path); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	mapper := &settingsMapperStub{}
	pool := device.NewPoolWithDynamicInterfaceMapper(&config.Config{}, mapper)
	server := &Server{pool: pool, fullCfg: &config.Config{}, configPath: path}

	response := updateSystemSettings(t, server, true)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !mapper.enabled || !loaded.System.OpenWRTDynamicInterfaces || !server.fullCfg.System.OpenWRTDynamicInterfaces {
		t.Fatalf("mapper=%v loaded=%v runtime=%v", mapper.enabled, loaded.System, server.fullCfg.System)
	}
}

func TestSystemSettingsHandlerRejectsUnsupportedSystemWithoutPersisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := writeSystemSettingsFixture(path); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	mapper := &settingsMapperStub{validateErr: errors.New("当前系统不是 OpenWrt")}
	pool := device.NewPoolWithDynamicInterfaceMapper(&config.Config{}, mapper)
	server := &Server{pool: pool, fullCfg: &config.Config{}, configPath: path}

	response := updateSystemSettings(t, server, true)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if mapper.enabled || loaded.System.OpenWRTDynamicInterfaces {
		t.Fatalf("unsupported setting was applied: mapper=%v config=%v", mapper.enabled, loaded.System)
	}
}

func TestSystemSettingsHandlerRollsBackRuntimeWhenPersistenceFails(t *testing.T) {
	mapper := &settingsMapperStub{}
	pool := device.NewPoolWithDynamicInterfaceMapper(&config.Config{}, mapper)
	server := &Server{
		pool:       pool,
		fullCfg:    &config.Config{},
		configPath: filepath.Join(t.TempDir(), "missing", "config.yaml"),
	}

	response := updateSystemSettings(t, server, true)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if mapper.enabled {
		t.Fatal("runtime setting was not rolled back")
	}
}

func updateSystemSettings(t *testing.T, server *Server, enabled bool) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(systemSettingsPayload{OpenWRTDynamicInterfaces: enabled})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/settings/system", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	server.handleUpdateSystemSettings(ctx)
	return recorder
}

func writeSystemSettingsFixture(path string) error {
	data := []byte("system:\n  openwrt_dynamic_interfaces: false\ndevices: []\n")
	return os.WriteFile(path, data, 0o600)
}
