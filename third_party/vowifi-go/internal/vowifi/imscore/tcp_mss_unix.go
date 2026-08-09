//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package imscore

import (
	"net"
	"syscall"
)

func setIPSec3GPPTCPMSS(conn net.Conn) error {
	return setRawTCPMSS(conn)
}

func setRawTCPMSS(socket any) error {
	connection, ok := socket.(syscall.Conn)
	if !ok {
		return nil
	}
	raw, err := connection.SyscallConn()
	if err != nil {
		return err
	}
	var socketErr error
	if err := raw.Control(func(fd uintptr) {
		socketErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_MAXSEG, ipsec3gppTCPMSS)
	}); err != nil {
		return err
	}
	return socketErr
}
