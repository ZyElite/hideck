//go:build !linux

package ipsec

import (
	"errors"
	"net"
	"syscall"
)

// setUDPEncap is not supported outside Linux (UDP_ENCAP is Linux-only).
func setUDPEncap(conn *net.UDPConn, enable bool) error {
	return errors.New("UDP encapsulation is not supported on this platform")
}

// startErrorListener is a no-op outside Linux (IP_RECVERR/MSG_ERRQUEUE are
// Linux-specific).
func (r *SocketManager) startErrorListener() {
	defer r.wg.Done()
}

// SockExtendedErr mirrors syscall.SockExtendedErr for API compatibility.
type SockExtendedErr struct {
	Errno  uint32
	Origin uint8
	Type   uint8
	Code   uint8
	Info   uint32
	Data   uint32
}

// ParseSockExtError is not supported outside Linux.
func ParseSockExtError(b []byte) (*SockExtendedErr, error) {
	return nil, errors.New("extended socket errors are not supported on this platform")
}

// soReusePort is SO_REUSEPORT on non-Linux platforms.
const soReusePort = syscall.SO_REUSEPORT
