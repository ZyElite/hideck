// Package ipsec implements ESP and the UDP transports used by SWu.
package ipsec

import "net"

// NetEventType identifies an asynchronous network-path change.
type NetEventType int

const (
	EventPathMTU NetEventType = iota
	EventNetworkDown
	EventNATPortChanged

	// Compatibility aliases retained for callers of the reconstructed API.
	NetEventMTU         = EventPathMTU
	NetEventUnreachable = EventNetworkDown
	NetEventPortChanged = EventNATPortChanged
)

// NetEvent is delivered when the socket error queue or NAT rebinding changes
// the active network path.
type NetEvent struct {
	Type    NetEventType
	PMTU    uint32
	Reason  string
	OldPort int
	NewPort int
}

// Socks5Config contains the complete legacy SOCKS5 transport configuration.
type Socks5Config struct {
	ProxyAddr  string
	Username   string
	Password   string
	RemoteAddr string
	DNSServer  string
	DeviceID   string
}

// Transport is the legacy SWu IKE/ESP network contract.
type Transport interface {
	ESPPackets() <-chan []byte
	IKEPackets() <-chan []byte
	LocalAddrString() string
	LocalIP() net.IP
	LocalPort() uint16
	NetEventsChan() <-chan NetEvent
	RawFD() (int, error)
	RemoteAddrString() string
	RemoteIP() net.IP
	RemotePort() int
	SendESP([]byte) error
	SendIKE([]byte) error
	SendNATKeepalive() error
	SetRemotePort(int)
	SetUDPEncap() error
	Start() error
	Stop()
}
