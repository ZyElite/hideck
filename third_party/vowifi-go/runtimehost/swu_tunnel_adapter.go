package runtimehost

import (
	"net"

	"github.com/iniwex5/vowifi-go/engine/swu"
)

type swuTunnelAdapter struct {
	*swu.Session
}

func (adapter *swuTunnelAdapter) UpdateAddresses(oldIP, newIP net.IP) error {
	return adapter.Session.UpdateAddresses(oldIP, newIP)
}
