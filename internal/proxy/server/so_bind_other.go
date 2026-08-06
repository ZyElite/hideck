//go:build !linux

package server

// soBindToDevice is a no-op placeholder on non-Linux platforms.
const soBindToDevice = 0
