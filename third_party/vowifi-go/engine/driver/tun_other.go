//go:build !linux

package driver

import "errors"

// TUNDevice is a non-Linux TUN device stub.
type TUNDevice struct {
	name string
}

// NewTUNDevice is unsupported off Linux.
func NewTUNDevice(name string) (*TUNDevice, error) {
	return nil, errUnsupportedPlatform
}

// Read is unsupported off Linux.
func (d *TUNDevice) Read(b []byte) (int, error) { return 0, errUnsupportedPlatform }

// Write is unsupported off Linux.
func (d *TUNDevice) Write(b []byte) (int, error) { return 0, errUnsupportedPlatform }

// Close is a no-op off Linux.
func (d *TUNDevice) Close() error { return nil }

// DeviceName returns the configured name.
func (d *TUNDevice) DeviceName() string {
	if d == nil {
		return ""
	}
	return d.name
}

var _ = errors.New
