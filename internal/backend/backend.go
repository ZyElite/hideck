// Package backend 定义 VoHive 设备后端的统一抽象层。
// AT、QMI、MBIM 和 PC/SC 通过配置开关 device_backend 选择。
package backend

// DeviceBackend 顶层聚合接口，所有设备后端均实现此接口。
type DeviceBackend interface {
	DeviceInfoProvider
	SMSProvider
	OperatingModeController
	SIMAuthProvider

	// Mode 返回当前后端模式标识。
	Mode() string

	// Close 释放后端持有的资源（QMI service 连接等）
	Close() error
}
