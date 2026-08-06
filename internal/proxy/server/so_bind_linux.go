//go:build linux

package server

import "syscall"

// soBindToDevice is SO_BINDTODEVICE on Linux.
const soBindToDevice = syscall.SO_BINDTODEVICE
