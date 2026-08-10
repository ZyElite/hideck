package media

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"syscall"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

const voiceDSCP = 46

func packetConnAddrString(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	return addr.String()
}

func addrString(addr *net.UDPAddr) string {
	if addr == nil {
		return ""
	}
	return addr.String()
}

func packetConnAddrToUDPAddr(addr net.Addr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	if udpAddr, ok := addr.(*net.UDPAddr); ok {
		return cloneUDPAddr(udpAddr)
	}
	resolved, err := net.ResolveUDPAddr("udp", addr.String())
	if err != nil {
		return nil
	}
	return resolved
}

func packetConnUDPAddr(conn net.PacketConn) *net.UDPAddr {
	if conn == nil {
		return nil
	}
	return packetConnAddrToUDPAddr(conn.LocalAddr())
}

func listenIMSPacket(listener imsendpoint.PacketListener, addr *net.UDPAddr) (net.PacketConn, error) {
	if listener != nil {
		return listener.ListenPacket(context.Background(), "udp", addr)
	}
	return net.ListenUDP(udpNetwork(addr.IP), addr)
}

func listenLANRTPPair(address string, start, end int) (*net.UDPConn, *net.UDPConn, error) {
	bindIP := net.ParseIP(strings.Trim(strings.TrimSpace(address), "[]"))
	if bindIP == nil {
		bindIP = net.IPv4zero
	}
	if start > 0 && end >= start {
		return tryListenLANPortRange(bindIP, start, end)
	}
	return listenEphemeralLANPair(bindIP)
}

func listenEphemeralLANPair(bindIP net.IP) (*net.UDPConn, *net.UDPConn, error) {
	const attempts = 100
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		rtp, err := net.ListenUDP(udpNetwork(bindIP), &net.UDPAddr{IP: bindIP})
		if err != nil {
			return nil, nil, fmt.Errorf("media: bind LAN RTP: %w", err)
		}
		rtpPort := rtp.LocalAddr().(*net.UDPAddr).Port
		rtcp, err := net.ListenUDP(udpNetwork(bindIP), &net.UDPAddr{IP: bindIP, Port: rtpPort + 1})
		if err == nil {
			return rtp, rtcp, nil
		}
		lastErr = err
		_ = rtp.Close()
	}
	return nil, nil, fmt.Errorf("media: bind consecutive LAN RTP/RTCP ports: %w", lastErr)
}

func tryListenLANPortRange(ip net.IP, start, end int) (*net.UDPConn, *net.UDPConn, error) {
	var lastErr error
	for port := start; port < end; port++ {
		rtp, err := net.ListenUDP(udpNetwork(ip), &net.UDPAddr{IP: ip, Port: port})
		if err != nil {
			lastErr = err
			continue
		}
		rtcp, err := net.ListenUDP(udpNetwork(ip), &net.UDPAddr{IP: ip, Port: port + 1})
		if err == nil {
			return rtp, rtcp, nil
		}
		lastErr = err
		_ = rtp.Close()
	}
	return nil, nil, fmt.Errorf("media: no consecutive LAN RTP/RTCP ports in %d-%d: %w", start, end, lastErr)
}

func udpNetwork(ip net.IP) string {
	if ip != nil && ip.To4() == nil {
		return "udp6"
	}
	return "udp4"
}

func setDSCP(conn net.PacketConn) error {
	syscallConn, ok := conn.(interface {
		SyscallConn() (syscall.RawConn, error)
	})
	if !ok {
		return errors.New("packet connection does not expose syscall.RawConn")
	}
	raw, err := syscallConn.SyscallConn()
	if err != nil {
		return fmt.Errorf("get raw connection: %w", err)
	}
	var ipv4Err, ipv6Err error
	if err := raw.Control(func(fd uintptr) {
		ipv4Err = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_TOS, voiceDSCP<<2)
		ipv6Err = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, syscall.IPV6_TCLASS, voiceDSCP<<2)
	}); err != nil {
		return err
	}
	if ipv4Err != nil && ipv6Err != nil {
		return fmt.Errorf("IPv4 TOS: %v, IPv6 TCLASS: %v", ipv4Err, ipv6Err)
	}
	return nil
}

func joinUDPAddress(host string, port int) string {
	return net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))
}
