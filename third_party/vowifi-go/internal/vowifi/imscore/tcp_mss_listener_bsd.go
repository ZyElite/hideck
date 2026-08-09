//go:build darwin || freebsd || openbsd || netbsd || dragonfly

package imscore

import "net"

func setIPSec3GPPListenerTCPMSS(net.Listener) error {
	return nil
}
