//go:build !linux

package netprobe

// soBindToDevice is a no-op placeholder on non-Linux platforms.
const soBindToDevice = 0
