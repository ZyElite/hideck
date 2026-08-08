//go:build linux

package ipsec

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// SockExtendedErr mirrors syscall.SockExtendedErr (Linux IP_RECVERR ancillary
// data) so the type stays portable.
type SockExtendedErr = unix.SockExtendedErr

// ParseSockExtError extracts the extended socket error (IP_RECVERR) carried in
// a received ancillary message buffer. Only ICMP-originated errors (level
// SOL_IP / IP_RECVERR, or IPPROTO_IPV6 / IPV6_RECVERR) are reported.
func ParseSockExtError(b []byte) (*SockExtendedErr, error) {
	cmsgs, err := syscall.ParseSocketControlMessage(b)
	if err != nil {
		return nil, err
	}
	for _, cm := range cmsgs {
		h := cm.Header
		if !((h.Level == syscall.SOL_IP && h.Type == syscall.IP_RECVERR) ||
			(h.Level == syscall.IPPROTO_IPV6 && h.Type == 25 /*IPV6_RECVERR*/)) {
			continue
		}
		if len(cm.Data) < int(unsafe.Sizeof(unix.SockExtendedErr{})) {
			continue
		}
		value := *(*unix.SockExtendedErr)(unsafe.Pointer(&cm.Data[0]))
		return &value, nil
	}
	return nil, errors.New("no extended socket error")
}

// setUDPEncap toggles UDP encapsulation (UDP_ENCAP_ESPINUDP) on the socket,
// which is how ESP-in-UDP (RFC 3948) is exposed to the kernel for sockets
// that carry both IKE and ESP on port 4500.
func setUDPEncap(conn *net.UDPConn, enable bool) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	err = raw.Control(func(fd uintptr) {
		v := 0
		if enable {
			v = 2 // UDP_ENCAP_ESPINUDP
		}
		serr = syscall.SetsockoptInt(int(fd), unix.SOL_UDP, 100 /*UDP_ENCAP*/, v)
	})
	if err != nil {
		return err
	}
	return serr
}

// startErrorListener enables IP_RECVERR on the socket and drains the error
// queue, turning ICMP errors into NetEvents.
func (r *SocketManager) startErrorListener() {
	defer r.wg.Done()
	if r.Conn == nil {
		return
	}
	raw, err := r.Conn.SyscallConn()
	if err != nil {
		logWarn("error listener: " + err.Error())
		return
	}
	ipv6 := false
	if r.LocalAddr != nil {
		ipv6 = r.LocalAddr.IP.To4() == nil && r.LocalAddr.IP.To16() != nil
	}
	var fd int
	var ctrlErr error
	err = raw.Control(func(f uintptr) {
		fd = int(f)
		if ipv6 {
			ctrlErr = syscall.SetsockoptInt(fd, syscall.IPPROTO_IPV6, 25 /*IPV6_RECVERR*/, 1)
		} else {
			ctrlErr = syscall.SetsockoptInt(fd, syscall.SOL_IP, syscall.IP_RECVERR, 1)
		}
	})
	if err != nil || ctrlErr != nil {
		logWarn(fmt.Sprintf("error listener: enable IP_RECVERR: %v", err))
		return
	}

	pkt := make([]byte, 1024)
	oob := make([]byte, 1024)
	for {
		select {
		case <-r.closeChan:
			return
		default:
		}
		pollFDs := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLERR}}
		if _, err := unix.Poll(pollFDs, 250); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return
		}
		if pollFDs[0].Revents&unix.POLLERR == 0 {
			continue
		}
		_, oobn, _, _, err := syscall.Recvmsg(fd, pkt, oob, syscall.MSG_ERRQUEUE|syscall.MSG_DONTWAIT)
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				continue
			}
			return
		}
		if oobn < 1 {
			continue
		}
		sockext, err := ParseSockExtError(oob[:oobn])
		if err != nil || sockext == nil {
			continue
		}
		if sockext.Origin != unix.SO_EE_ORIGIN_ICMP && sockext.Origin != unix.SO_EE_ORIGIN_ICMP6 {
			continue // not ICMP-originated
		}
		if event, ok := netEventFromExtendedError(sockext.Errno, sockext.Info); ok {
			r.sendNetEvent(event)
		}
	}
}

func (r *SocketManager) sendNetEvent(ev NetEvent) {
	select {
	case r.NetEvents <- ev:
	default:
	}
}

// soReusePort is SO_REUSEPORT on Linux (not exposed by the syscall package).
const soReusePort = 15
