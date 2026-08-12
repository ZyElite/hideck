//go:build darwin

package pcsc

func systemUSBPath(uint32) (string, bool) { return "", false }

func enrichUSBReader(*Reader) {}
