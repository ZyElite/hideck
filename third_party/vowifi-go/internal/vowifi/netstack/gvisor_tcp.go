package netstack

import (
	"context"
	"errors"
	"fmt"
	"net"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/waiter"
)

const (
	imsTCPMSS        = 1100
	imsIPv4LinkMTU   = imsTCPMSS + header.IPv4MinimumSize + header.TCPMinimumSize
	imsIPv6LinkMTU   = header.IPv6MinimumMTU
	tcpListenBacklog = 4096
)

type tcpDialConfig struct {
	local    tcpip.FullAddress
	remote   tcpip.FullAddress
	protocol tcpip.NetworkProtocolNumber
}

func imsLinkMTU(protocol tcpip.NetworkProtocolNumber) uint32 {
	if protocol == ipv4.ProtocolNumber {
		return imsIPv4LinkMTU
	}
	return imsIPv6LinkMTU
}

func dialTCPWithMSS(ctx context.Context, networkStack *stack.Stack, cfg tcpDialConfig) (*gonet.TCPConn, error) {
	var queue waiter.Queue
	endpoint, err := networkStack.NewEndpoint(tcp.ProtocolNumber, cfg.protocol, &queue)
	if err != nil {
		return nil, errors.New(err.String())
	}
	if err := endpoint.SetSockOptInt(tcpip.MaxSegOption, imsTCPMSS); err != nil {
		endpoint.Close()
		return nil, fmt.Errorf("set IMS TCP MSS: %s", err)
	}
	if cfg.local != (tcpip.FullAddress{}) {
		if err := endpoint.Bind(cfg.local); err != nil {
			endpoint.Close()
			return nil, fmt.Errorf("bind IMS TCP endpoint: %s", err)
		}
	}
	if err := connectTCPEndpoint(ctx, endpoint, &queue, cfg.remote); err != nil {
		endpoint.Close()
		return nil, err
	}
	return gonet.NewTCPConn(&queue, endpoint), nil
}

func connectTCPEndpoint(ctx context.Context, endpoint tcpip.Endpoint, queue *waiter.Queue, remote tcpip.FullAddress) error {
	entry, ready := waiter.NewChannelEntry(waiter.WritableEvents)
	queue.EventRegister(&entry)
	defer queue.EventUnregister(&entry)

	err := endpoint.Connect(remote)
	if _, pending := err.(*tcpip.ErrConnectStarted); pending {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ready:
			err = endpoint.LastError()
		}
	}
	if err == nil {
		return nil
	}
	return &net.OpError{Op: "connect", Net: "tcp", Addr: tcpAddress(remote), Err: errors.New(err.String())}
}

func listenTCPWithMSS(networkStack *stack.Stack, address tcpip.FullAddress, protocol tcpip.NetworkProtocolNumber) (*gonet.TCPListener, error) {
	var queue waiter.Queue
	endpoint, err := networkStack.NewEndpoint(tcp.ProtocolNumber, protocol, &queue)
	if err != nil {
		return nil, errors.New(err.String())
	}
	if err := endpoint.SetSockOptInt(tcpip.MaxSegOption, imsTCPMSS); err != nil {
		endpoint.Close()
		return nil, fmt.Errorf("set IMS TCP listener MSS: %s", err)
	}
	if err := endpoint.Bind(address); err != nil {
		endpoint.Close()
		return nil, fmt.Errorf("bind IMS TCP listener: %s", err)
	}
	if err := endpoint.Listen(tcpListenBacklog); err != nil {
		endpoint.Close()
		return nil, fmt.Errorf("listen on IMS TCP endpoint: %s", err)
	}
	return gonet.NewTCPListener(networkStack, &queue, endpoint), nil
}

func tcpAddress(address tcpip.FullAddress) *net.TCPAddr {
	return &net.TCPAddr{IP: net.IP(address.Addr.AsSlice()), Port: int(address.Port)}
}
