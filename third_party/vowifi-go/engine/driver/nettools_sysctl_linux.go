//go:build linux

package driver

import (
	"fmt"
	"os"
)

func (n *NetTools) SetSysctl(key, value string) error {
	path := sysctlPath(key)
	if path == "" {
		return fmt.Errorf("无效的 sysctl key: %q", key)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		return fmt.Errorf("设置 sysctl %s=%s 失败: %v", key, value, err)
	}
	return nil
}

func (n *NetTools) EnsureIPv6Enabled(iface string) ([]string, error) {
	return ensureIPv6Enabled(iface, readSysctlValue, n.SetSysctl)
}
