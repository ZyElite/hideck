package policy

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/common"
	"gopkg.in/yaml.v3"
)

const defaultCarrierOverridesPath = "config/carrier_overrides.yaml"

type carrierOverridesFile struct {
	CarrierOverrides map[string]CarrierOverride `yaml:"carrier_overrides"`
}

func LoadAndSetCarrierOverridesFile(path string) (resolvedPath string, count int, missing bool, err error) {
	resolvedPath = strings.TrimSpace(path)
	if resolvedPath == "" {
		resolvedPath = defaultCarrierOverridesPath
	}
	if _, statErr := os.Stat(resolvedPath); statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return resolvedPath, 0, false, fmt.Errorf("stat carrier overrides: %w", statErr)
	}
	values, err := LoadCarrierOverridesFile(resolvedPath)
	if err != nil {
		return resolvedPath, 0, false, err
	}
	if err := SetCarrierOverrides(values); err != nil {
		return resolvedPath, 0, false, err
	}
	_, statErr := os.Stat(resolvedPath)
	return resolvedPath, len(values), errors.Is(statErr, os.ErrNotExist), nil
}

func LoadCarrierOverridesFile(path string) (map[string]CarrierOverride, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("carrier overrides path is empty")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]CarrierOverride{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read carrier overrides: %w", err)
	}
	var wrapped carrierOverridesFile
	if err := yaml.Unmarshal(data, &wrapped); err != nil {
		return nil, fmt.Errorf("parse carrier overrides: %w", err)
	}
	values := wrapped.CarrierOverrides
	if values == nil {
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, fmt.Errorf("parse carrier overrides: %w", err)
		}
	}
	result := make(map[string]CarrierOverride, len(values))
	for _, rawKey := range sortedRootKeys(values) {
		mcc, mnc, ok := parsePLMNKey(rawKey)
		if !ok {
			return nil, fmt.Errorf("invalid carrier override PLMN key %q", rawKey)
		}
		result[plmnKey(mcc, mnc)] = NormalizeCarrierOverride(values[rawKey])
	}
	return result, nil
}

func sortedRootKeys(values map[string]CarrierOverride) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func parsePLMNKey(key string) (mcc, mnc string, ok bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}
	if strings.Contains(key, "-") {
		parts := strings.SplitN(key, "-", 2)
		if len(parts) != 2 || !isDigits(strings.TrimSpace(parts[0])) || !isDigits(strings.TrimSpace(parts[1])) {
			return "", "", false
		}
		return common.Plmn3(parts[0]), common.Plmn3(parts[1]), true
	}
	if !isDigits(key) || (len(key) != 5 && len(key) != 6) {
		return "", "", false
	}
	return common.Plmn3(key[:3]), common.Plmn3(key[3:]), true
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
