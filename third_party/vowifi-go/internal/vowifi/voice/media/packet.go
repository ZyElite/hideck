package media

import (
	"errors"
	"net"
	"sync/atomic"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const (
	pcapDirectionIMSToLAN byte = 0
	pcapDirectionLANToIMS byte = 1
)

func (r *RTPRelay) handleIMSPacket(packet []byte, source *net.UDPAddr) {
	r.writePCAPPacket(packet, pcapDirectionIMSToLAN)
	remote := r.remoteAddr.Load()
	if remote != nil && !remote.IP.Equal(source.IP) {
		deviceID, _ := r.logContext()
		logging.WarnRate("media-ims-source:"+deviceID, 5*time.Second,
			"RTP IMS source does not match negotiated peer", "source", source, "expected", remote)
		return
	}
	client := r.clientAddr.Load()
	if client == nil {
		deviceID, _ := r.logContext()
		logging.WarnRate("media-client-missing:"+deviceID, 5*time.Second,
			"RTP packet arrived before client address was set", "source", source)
		return
	}
	r.applyIMSPayloadTypeMapping(packet)
	if err := writePacket(r.lanRTPConn(), packet, client); err != nil {
		r.logWriteError("LAN", err)
		return
	}
	atomic.AddUint64(&r.bytesIMSToLAN, uint64(len(packet)))
	atomic.CompareAndSwapUint32(&r.imsFirstPacket, 0, 1)
	if monitor := r.monitorSnapshot(); monitor != nil {
		monitor.UpdateIMS()
	}
}

func (r *RTPRelay) handleLANPacket(packet []byte, source *net.UDPAddr) {
	r.writePCAPPacket(packet, pcapDirectionLANToIMS)
	learnAddress(&r.clientAddr, source)
	if r.clientAddrRTCP.Load() == nil {
		r.clientAddrRTCP.Store(offsetUDPAddr(source, 1))
	}
	remote := r.remoteAddr.Load()
	if remote == nil {
		deviceID, _ := r.logContext()
		logging.WarnRate("media-ims-missing:"+deviceID, 5*time.Second,
			"RTP packet arrived before IMS address was set", "source", source)
		return
	}
	r.applyLANPayloadTypeMapping(packet)
	if err := writePacket(r.connIMS, packet, remote); err != nil {
		r.logWriteError("IMS", err)
		return
	}
	atomic.AddUint64(&r.bytesLANToIMS, uint64(len(packet)))
	atomic.CompareAndSwapUint32(&r.lanFirstPacket, 0, 1)
	if monitor := r.monitorSnapshot(); monitor != nil {
		monitor.UpdateLAN()
	}
}

func learnAddress(destination *atomic.Pointer[net.UDPAddr], source *net.UDPAddr) {
	current := destination.Load()
	if current != nil && current.IP.Equal(source.IP) && current.Port == source.Port && current.Zone == source.Zone {
		return
	}
	destination.Store(cloneUDPAddr(source))
}

func writePacket(conn net.PacketConn, packet []byte, addr *net.UDPAddr) error {
	if conn == nil {
		return errors.New("media socket is unavailable")
	}
	if addr == nil {
		return errors.New("media destination is unavailable")
	}
	_, err := conn.WriteTo(packet, addr)
	return err
}

func (r *RTPRelay) logWriteError(side string, err error) {
	deviceID, traceID := r.logContext()
	logging.WarnRate("media-write:"+side+":"+deviceID, time.Second,
		"RTP relay write failed", "side", side, "device", deviceID, "trace", traceID, "error", err)
}

func (r *RTPRelay) applyIMSPayloadTypeMapping(packet []byte) {
	applyPayloadTypeMapping(packet, r.ptMap.Load(), true)
}

func (r *RTPRelay) applyLANPayloadTypeMapping(packet []byte) {
	applyPayloadTypeMapping(packet, r.ptMap.Load(), false)
}

func applyPayloadTypeMapping(packet []byte, mapping *ptMapping, fromIMS bool) {
	if len(packet) < 2 || packet[0]&0xc0 != 0x80 || mapping == nil {
		return
	}
	payloadType := int(packet[1] & 0x7f)
	selected := mapping.lanToIms
	if fromIMS {
		selected = mapping.imsToLan
	}
	replacement, exists := selected[payloadType]
	if !exists {
		return
	}
	packet[1] = packet[1]&0x80 | byte(replacement&0x7f)
}
