//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package imscore

import (
	"errors"
	"net"
)

var errIPSecTCPMSSUnsupported = errors.New("imscore: TCP_MAXSEG is unsupported on this platform")

func setIPSec3GPPTCPMSS(net.Conn) error {
	return errIPSecTCPMSSUnsupported
}

func setIPSec3GPPListenerTCPMSS(net.Listener) error {
	return errIPSecTCPMSSUnsupported
}
