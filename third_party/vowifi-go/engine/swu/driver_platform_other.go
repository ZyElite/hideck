//go:build !linux

package swu

import "fmt"

func validatePlatformDataplaneMode(mode string) error {
	if mode == DataplaneModeUserspace {
		return nil
	}
	return fmt.Errorf("swu: %s data plane requires linux", mode)
}
