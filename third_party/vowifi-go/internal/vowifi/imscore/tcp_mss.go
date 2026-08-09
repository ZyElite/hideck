package imscore

import (
	"fmt"
	"net"
	"strings"
)

const ipsec3gppTCPMSS = 1100

func ipsec3gppTCPNetwork(network string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(network)), "tcp")
}

type ipsec3gppMSSListener struct {
	net.Listener
}

func (listener *ipsec3gppMSSListener) Accept() (net.Conn, error) {
	conn, err := listener.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return applyIPSec3GPPAcceptedTCPMSS(conn)
}

func applyIPSec3GPPAcceptedTCPMSS(conn net.Conn) (net.Conn, error) {
	if conn == nil {
		return nil, nil
	}
	if err := setIPSec3GPPTCPMSS(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("imscore: set accepted IPsec TCP MSS: %w", err)
	}
	return conn, nil
}

func applyIPSec3GPPPortSListenerTCPMSSWithError(listener net.Listener) (net.Listener, error) {
	if listener == nil {
		return nil, nil
	}
	if err := setIPSec3GPPListenerTCPMSS(listener); err != nil {
		return nil, fmt.Errorf("imscore: set IPsec listener TCP MSS: %w", err)
	}
	return &ipsec3gppMSSListener{Listener: listener}, nil
}
