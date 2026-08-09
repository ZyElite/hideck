//go:build linux

package imscore

import "net"

func setIPSec3GPPListenerTCPMSS(listener net.Listener) error {
	return setRawTCPMSS(listener)
}
