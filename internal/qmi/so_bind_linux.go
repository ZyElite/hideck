//go:build linux

package qmicore

import "syscall"

// soBindToDevice is SO_BINDTODEVICE on Linux.
const soBindToDevice = syscall.SO_BINDTODEVICE
