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
type SockExtendedErr struct {
	Errno  uint8
	Origin uint8
	Type   uint8
	Code   uint8
	Info   uint32
	Data   uint32
}

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
		if len(cm.Data) <= 15 {
			continue
		}
		e := new(SockExtendedErr)
		copy((*[16]byte)(unsafe.Pointer(e))[:], cm.Data[:16])
		return e, nil
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
	if r.conn == nil {
		return
	}
	raw, err := r.conn.SyscallConn()
	if err != nil {
		logWarn("error listener: " + err.Error())
		return
	}
	ipv6 := false
	if r.remoteAddr != nil {
		ipv6 = r.remoteAddr.IP.To4() == nil && r.remoteAddr.IP.To16() != nil
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

	pkt := make([]byte, 0x400)
	oob := make([]byte, 0x400)
	for {
		select {
		case <-r.stop:
			return
		default:
		}
		// MSG_ERRQUEUE reads errors queued on the socket; MSG_DONTWAIT keeps
		// the loop responsive to Stop.
		_, oobn, _, _, err := syscall.Recvmsg(fd, pkt, oob, syscall.MSG_ERRQUEUE|syscall.MSG_DONTWAIT)
		if err != nil {
			continue
		}
		if oobn < 1 {
			continue
		}
		sockext, err := ParseSockExtError(oob[:oobn])
		if err != nil || sockext == nil {
			continue
		}
		if sockext.Origin|1 != 3 {
			continue // not ICMP-originated
		}
		switch sockext.Errno {
		case 90: // EMSGSIZE // "fragmentation needed": MTU update
			if uint32(sockext.Info)-0x1f5 < 999 {
				r.sendNetEvent(NetEvent{Type: NetEventMTU, Param: sockext.Info, Detail: fmt.Sprintf("MTU changed to %d", sockext.Info)})
			}
		case 101, 113: // ENETUNREACH, EHOSTUNREACH
			r.sendNetEvent(NetEvent{Type: NetEventUnreachable, Detail: fmt.Sprintf("ICMP error %d", sockext.Errno)})
		}
	}
}

func (r *SocketManager) sendNetEvent(ev NetEvent) {
	select {
	case r.netEvents <- ev:
	default:
	}
}

// soReusePort is SO_REUSEPORT on Linux (not exposed by the syscall package).
const soReusePort = 15
