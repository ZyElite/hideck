//go:build linux

package pcsc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func systemUSBPath(channel uint32) (string, bool) {
	if channel>>16 != 0x0020 {
		return "", false
	}
	bus, device := int((channel>>8)&0xFF), int(channel&0xFF)
	entries, err := os.ReadDir("/sys/bus/usb/devices")
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		path := filepath.Join("/sys/bus/usb/devices", entry.Name())
		if readSysfsInt(path, "busnum") == bus && readSysfsInt(path, "devnum") == device {
			return entry.Name(), true
		}
	}
	return "", false
}

func enrichUSBReader(reader *Reader) {
	reader.VendorID = readSysfsText(reader.USBPath, "idVendor")
	reader.ProductID = readSysfsText(reader.USBPath, "idProduct")
	reader.Manufacturer = readSysfsText(reader.USBPath, "manufacturer")
	if product := readSysfsText(reader.USBPath, "product"); product != "" {
		reader.Product = product
	}
}

func readSysfsText(usbPath, name string) string {
	value, err := os.ReadFile(filepath.Join("/sys/bus/usb/devices", usbPath, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(value))
}

func readSysfsInt(path, name string) int {
	value, err := os.ReadFile(filepath.Join(path, name))
	if err != nil {
		return -1
	}
	result, err := strconv.Atoi(strings.TrimSpace(string(value)))
	if err != nil {
		return -1
	}
	return result
}
