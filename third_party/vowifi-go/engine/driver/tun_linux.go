//go:build linux

package driver

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// TUN device ioctl constants (linux/if_tun.h).
const (
	tunSetIFF   = 0x400454ca // TUNSETIFF
	tunGetIFF   = 0x800454d2 // TUNGETIFF
	ifnamsiz    = 16
	iffTUN      = 0x0001 // IFF_TUN
	iffNOPI     = 0x1000 // IFF_NO_PI
	iffMULTIQUEUE = 0x0100
)

// ifreq mirrors struct ifreq for the TUNSETIFF ioctl.
type ifreq struct {
	name  [ifnamsiz]byte
	flags uint16
	_     [22]byte
}

// TUNDevice is a Linux TUN device.
type TUNDevice struct {
	file *os.File
	name string
}

// NewTUNDevice creates a TUN device with the given name ("" for an
// auto-assigned name).
func NewTUNDevice(name string) (*TUNDevice, error) {
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &NetToolError{Op: "open /dev/net/tun", Err: err}
	}
	req := &ifreq{flags: iffTUN | iffNOPI}
	if name != "" {
		copy(req.name[:], name)
	}
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), tunSetIFF, uintptr(unsafe.Pointer(req))); errno != 0 {
		unix.Close(fd)
		return nil, &NetToolError{Op: "TUNSETIFF", Err: errno}
	}
	devName := name
	if devName == "" {
		devName = cString(req.name[:])
	}
	return &TUNDevice{file: os.NewFile(uintptr(fd), "tun"), name: devName}, nil
}

// Read reads a packet from the TUN device.
func (d *TUNDevice) Read(b []byte) (int, error) {
	if d == nil || d.file == nil {
		return 0, errors.New("driver: TUN device closed")
	}
	return d.file.Read(b)
}

// Write writes a packet to the TUN device.
func (d *TUNDevice) Write(b []byte) (int, error) {
	if d == nil || d.file == nil {
		return 0, errors.New("driver: TUN device closed")
	}
	return d.file.Write(b)
}

// Close closes the TUN device.
func (d *TUNDevice) Close() error {
	if d == nil || d.file == nil {
		return nil
	}
	err := d.file.Close()
	d.file = nil
	return err
}

// DeviceName returns the TUN device name.
func (d *TUNDevice) DeviceName() string {
	if d == nil {
		return ""
	}
	return d.name
}

// cString converts a NUL-terminated byte slice to a string.
func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
