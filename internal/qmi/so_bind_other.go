//go:build !linux

package qmicore

// soBindToDevice is a no-op placeholder on non-Linux platforms.
const soBindToDevice = 0
