package driver

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const sysctlRoot = "/proc/sys"

func ensureIPv6Enabled(
	iface string,
	read func(string) (string, error),
	write func(string, string) error,
) ([]string, error) {
	keys := []string{
		"net.ipv6.conf.all.disable_ipv6",
		"net.ipv6.conf.default.disable_ipv6",
	}
	if name := strings.TrimSpace(iface); name != "" {
		keys = append(keys, fmt.Sprintf("net.ipv6.conf.%s.disable_ipv6", name))
	}
	changed := make([]string, 0, len(keys))
	for _, key := range keys {
		value, err := read(key)
		if err != nil {
			return changed, fmt.Errorf("key=%s, cause=%w", key, err)
		}
		if value == "0" {
			continue
		}
		if value != "1" {
			return changed, fmt.Errorf("key=%s, cause=当前值=%q，期望为 0 或 1", key, value)
		}
		if err := write(key, "0"); err != nil {
			return changed, fmt.Errorf("key=%s, cause=%v, hint=需要 root/CAP_NET_ADMIN", key, err)
		}
		changed = append(changed, key)
		readBack, err := read(key)
		if err != nil {
			return changed, fmt.Errorf("key=%s, cause=%w", key, err)
		}
		if readBack != "0" {
			return changed, fmt.Errorf(
				"key=%s, cause=写入后回读值=%q, hint=需要 root/CAP_NET_ADMIN", key, readBack,
			)
		}
	}
	return changed, nil
}

func sysctlPath(key string) string {
	root := strings.TrimRight(sysctlRoot, "/")
	cleanKey, ok := normalizedSysctlKey(key)
	if !ok {
		return ""
	}
	cleanKey = strings.ReplaceAll(cleanKey, ".", "/")
	return root + "/" + cleanKey
}

func normalizedSysctlKey(key string) (string, bool) {
	key = strings.TrimSpace(key)
	if key == "" || strings.ContainsAny(key, "/\\\x00") {
		return "", false
	}
	for _, component := range strings.Split(key, ".") {
		if component == "" || component == "." || component == ".." {
			return "", false
		}
	}
	return key, true
}

func readSysctlValue(key string) (string, error) {
	path := sysctlPath(key)
	if path == "" {
		return "", fmt.Errorf("无效的 sysctl key: %q", key)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("内核/环境不支持 IPv6 sysctl: %s", key)
		}
		return "", fmt.Errorf("读取 sysctl %s 失败: %v", key, err)
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", fmt.Errorf("读取 sysctl %s 返回空值", key)
	}
	return value, nil
}
