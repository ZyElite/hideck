// Package ipsec implements the user-space ESP encapsulation and the UDP
// transports used by the vowifi SWu (UE <-> ePDG) client.
//
// Reconstructed from the decompiled engine/ipsec. It contains:
//
//   - SecurityAssociation: RFC 4303 ESP in tunnel mode, with AES-GCM (RFC
//     4106) or AES-CBC + a separate integrity transform.
//   - SocketManager: a direct UDP socket carrying IKE and ESP traffic, with
//     RFC 3948 NAT-T (the 4-byte non-ESP marker on port 4500), remote-address
//     rebinding and ICMP error reporting.
//   - Socks5Transport: the same traffic multiplexed over a SOCKS5 UDP
//     associate relay (RFC 1928/1929), used when the device has no direct
//     route to the ePDG.
package ipsec

import "net"

// NetEvent is an asynchronous network event delivered on a Transport's
// NetEventsChan. The SWu session consumes these to react to NAT rebinding
// (remote port change), ICMP errors and MTU updates.
type NetEvent struct {
	Type    uint32
	Param   uint32
	Detail  string
	OldPort uint32
	NewPort uint32
}

// NetEvent types.
const (
	// NetEventMTU reports an ICMP "packet too big" (Param = the new MTU).
	NetEventMTU uint32 = 0
	// NetEventUnreachable reports an ICMP host/network unreachable.
	NetEventUnreachable uint32 = 1
	// NetEventPortChanged reports that the remote NAT-T port changed.
	NetEventPortChanged uint32 = 2
)

// Socks5Config carries the optional SOCKS5 proxy credentials.
type Socks5Config struct {
	Username string
	Password string
}

// Transport is the network path used for IKE and ESP traffic between the SWu
// client and the ePDG.
type Transport interface {
	IKEPackets() <-chan []byte
	ESPPackets() <-chan []byte
	NetEventsChan() <-chan NetEvent
	Start()
	Stop()
	SendIKE(packet []byte)
	SendESP(packet []byte)
	SendNATKeepalive()
	SetRemotePort(port uint16)
	LocalIP() net.IP
	RemoteIP() net.IP
	LocalPort() uint16
	RemotePort() uint16
	LocalAddrString() string
	RemoteAddrString() string
	RawFD() (int, error)
	SetUDPEncap(enable bool) error
}
