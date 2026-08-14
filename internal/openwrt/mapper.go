package openwrt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

const (
	openWRTReleasePath = "/etc/openwrt_release"
	logicalNamePrefix  = "hideck_"
	maxCommandOutput   = 512
)

var (
	deviceIDPattern  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]{0,31}$`)
	interfacePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,31}$`)
)

type Environment interface {
	Validate() error
}

type Executor interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type Mapper struct {
	mu       sync.Mutex
	enabled  bool
	env      Environment
	executor Executor
	bindings map[string]string
}

func NewMapper(enabled bool) *Mapper {
	return NewMapperWithDependencies(enabled, systemEnvironment{}, commandExecutor{})
}

func NewMapperWithDependencies(enabled bool, env Environment, executor Executor) *Mapper {
	return &Mapper{
		enabled:  enabled,
		env:      env,
		executor: executor,
		bindings: make(map[string]string),
	}
}

func (m *Mapper) Enabled() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled
}

func (m *Mapper) SetEnabled(enabled bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.enabled = enabled
	m.mu.Unlock()
}

func (m *Mapper) Validate() error {
	if m == nil || m.env == nil || m.executor == nil {
		return errors.New("OpenWrt 动态接口映射器未初始化")
	}
	return m.env.Validate()
}

func (m *Mapper) Add(ctx context.Context, deviceID, dataInterface string) error {
	if m == nil {
		return errors.New("OpenWrt 动态接口映射器未初始化")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.enabled {
		return nil
	}
	if err := validateBinding(deviceID, dataInterface); err != nil {
		return err
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if m.bindings[deviceID] == dataInterface {
		return nil
	}
	if _, exists := m.bindings[deviceID]; exists {
		if err := m.removeLocked(ctx, deviceID); err != nil {
			return err
		}
	}

	payload, err := json.Marshal(map[string]any{
		"name":   LogicalName(deviceID),
		"proto":  "none",
		"device": dataInterface,
		"auto":   true,
	})
	if err != nil {
		return fmt.Errorf("序列化 OpenWrt 动态接口参数失败: %w", err)
	}
	if err := m.run(ctx, "call", "network", "add_dynamic", string(payload)); err != nil {
		return fmt.Errorf("添加 OpenWrt 动态接口 %s 失败: %w", LogicalName(deviceID), err)
	}
	m.bindings[deviceID] = dataInterface
	return nil
}

func (m *Mapper) Remove(ctx context.Context, deviceID string) error {
	if m == nil {
		return nil
	}
	if err := validateDeviceID(deviceID); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.bindings[deviceID]; !exists {
		return nil
	}
	if err := m.Validate(); err != nil {
		return err
	}
	return m.removeLocked(ctx, deviceID)
}

func (m *Mapper) removeLocked(ctx context.Context, deviceID string) error {
	target := "network.interface." + LogicalName(deviceID)
	if err := m.run(ctx, "call", target, "remove", "{}"); err != nil {
		return fmt.Errorf("移除 OpenWrt 动态接口 %s 失败: %w", LogicalName(deviceID), err)
	}
	delete(m.bindings, deviceID)
	return nil
}

func (m *Mapper) run(ctx context.Context, args ...string) error {
	output, err := m.executor.Run(ctx, "ubus", args...)
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(string(output))
	if len(detail) > maxCommandOutput {
		detail = detail[:maxCommandOutput]
	}
	if detail == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, detail)
}

func LogicalName(deviceID string) string {
	return logicalNamePrefix + deviceID
}

func validateBinding(deviceID, dataInterface string) error {
	if err := validateDeviceID(deviceID); err != nil {
		return err
	}
	if !interfacePattern.MatchString(dataInterface) {
		return fmt.Errorf("无效的数据网卡名: %q", dataInterface)
	}
	return nil
}

func validateDeviceID(deviceID string) error {
	if !deviceIDPattern.MatchString(deviceID) {
		return fmt.Errorf("无效的设备 ID: %q", deviceID)
	}
	return nil
}

type systemEnvironment struct{}

func (systemEnvironment) Validate() error {
	if _, err := os.Stat(openWRTReleasePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("当前系统不是 OpenWrt，不能启用动态接口映射")
		}
		return fmt.Errorf("检查 OpenWrt 系统标识失败: %w", err)
	}
	if _, err := exec.LookPath("ubus"); err != nil {
		return fmt.Errorf("OpenWrt 缺少 ubus 命令: %w", err)
	}
	return nil
}

type commandExecutor struct{}

func (commandExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
