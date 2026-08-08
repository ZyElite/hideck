//go:build linux

package driver

import (
	"fmt"

	"github.com/songgao/water"
)

func NewTUNDevice(name string) (*TUNDevice, error) {
	if name != "" {
		if err := deleteLinkIfExists(name); err != nil {
			return nil, fmt.Errorf("准备 TUN 设备 %s 失败: %w", name, err)
		}
	}
	config := water.Config{DeviceType: water.TUN}
	config.Name = name
	iface, err := water.New(config)
	if err != nil {
		return nil, fmt.Errorf("创建 TUN 设备失败: %w", err)
	}
	return &TUNDevice{iface: iface, Name: iface.Name()}, nil
}

func (t *TUNDevice) Read(packet []byte) (int, error) {
	return t.iface.Read(packet)
}

func (t *TUNDevice) Write(packet []byte) (int, error) {
	return t.iface.Write(packet)
}

func (t *TUNDevice) Close() error {
	return t.iface.Close()
}
