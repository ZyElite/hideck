//go:build linux

package netprobe

import "syscall"

// soBindToDevice is SO_BINDTODEVICE on Linux.
const soBindToDevice = syscall.SO_BINDTODEVICE
