package media

import (
	"encoding/binary"
	"net"
	"time"
)

// packetConnAddrString returns the string form of a packet conn address.
func packetConnAddrString(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	return addr.String()
}

// packetConnAddrToUDPAddr converts a net.Addr to a *net.UDPAddr.
func packetConnAddrToUDPAddr(addr net.Addr) *net.UDPAddr {
	if ua, ok := addr.(*net.UDPAddr); ok {
		return ua
	}
	return nil
}

// packetConnUDPAddr returns the local UDP address of a packet conn.
func packetConnUDPAddr(conn net.PacketConn) *net.UDPAddr {
	if conn == nil {
		return nil
	}
	return packetConnAddrToUDPAddr(conn.LocalAddr())
}

// tryListenLANPortRange binds a UDP socket on the first free port in the
// range [start, start+count).
func tryListenLANPortRange(ip net.IP, start, count int) (net.PacketConn, int, error) {
	for i := 0; i < count; i++ {
		port := start + i
		conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: ip, Port: port})
		if err == nil {
			return conn, port, nil
		}
	}
	// Fall back to an ephemeral port.
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: ip})
	if err != nil {
		return nil, 0, err
	}
	return conn, conn.LocalAddr().(*net.UDPAddr).Port, nil
}

// listenIMSPacket binds the IMS-side RTP socket.
func listenIMSPacket(ip net.IP, port int) (net.PacketConn, error) {
	return net.ListenUDP("udp", &net.UDPAddr{IP: ip, Port: port})
}

// listenLANRTPPair binds the LAN-side RTP socket.
func listenLANRTPPair(ip net.IP, start, count int) (net.PacketConn, int, error) {
	return tryListenLANPortRange(ip, start, count)
}

// applyIMSPayloadTypeMapping rewrites the payload type of an IMS->LAN packet.
func (r *RTPRelay) applyIMSPayloadTypeMapping(pkt []byte) {
	if r == nil || len(pkt) < 2 {
		return
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	// IMS PT -> LAN PT is the inverse of the LAN->IMS map.
	pt := int(pkt[1] & 0x7F)
	for lan, ims := range r.ptMapping {
		if ims == pt {
			pkt[1] = pkt[1]&0x80 | byte(lan)
			return
		}
	}
}

// applyLANPayloadTypeMapping rewrites the payload type of a LAN->IMS packet.
func (r *RTPRelay) applyLANPayloadTypeMapping(pkt []byte) {
	if r == nil || len(pkt) < 2 {
		return
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	pt := int(pkt[1] & 0x7F)
	if ims, ok := r.ptMapping[pt]; ok {
		pkt[1] = pkt[1]&0x80 | byte(ims)
	}
}

// handleIMSPacket processes one IMS-side RTP packet.
func (r *RTPRelay) handleIMSPacket(pkt []byte) {
	r.applyIMSPayloadTypeMapping(pkt)
	r.writePCAPPacket(pkt)
}

// handleLANPacket processes one LAN-side RTP packet.
func (r *RTPRelay) handleLANPacket(pkt []byte) {
	r.applyLANPayloadTypeMapping(pkt)
	r.writePCAPPacket(pkt)
}

// loopIMSRTCP relays RTCP from the IMS side.
func (r *RTPRelay) loopIMSRTCP() {
	buf := make([]byte, 2048)
	for {
		if r.shouldStop() {
			return
		}
		n, _, err := r.imsConn.ReadFrom(buf)
		if err != nil {
			if isRTPRelayReadClosedError(err) {
				return
			}
			continue
		}
		r.mu.RLock()
		lan := r.lanConn
		lanRemote := r.lanRemote
		r.mu.RUnlock()
		if lan == nil || lanRemote == nil {
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		_, _ = lan.WriteTo(pkt, lanRemote)
	}
}

// loopLANRTCP relays RTCP from the LAN side.
func (r *RTPRelay) loopLANRTCP() {
	buf := make([]byte, 2048)
	for {
		if r.shouldStop() {
			return
		}
		n, _, err := r.lanConn.ReadFrom(buf)
		if err != nil {
			if isRTPRelayReadClosedError(err) {
				return
			}
			continue
		}
		r.mu.RLock()
		ims := r.imsConn
		imsRemote := r.imsRemote
		r.mu.RUnlock()
		if ims == nil || imsRemote == nil {
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		_, _ = ims.WriteTo(pkt, imsRemote)
	}
}

// sendFakeRTCP sends a minimal RTCP receiver report to keep NAT alive.
func (r *RTPRelay) sendFakeRTCP() {
	if r == nil {
		return
	}
	r.mu.RLock()
	conn := r.imsConn
	remote := r.imsRemote
	r.mu.RUnlock()
	if conn == nil || remote == nil {
		return
	}
	// RTCP RR: version 2, no padding, RR count 0, packet type 201 (RR).
	pkt := make([]byte, 8)
	pkt[0] = 0x80
	pkt[1] = 201
	binary.BigEndian.PutUint16(pkt[2:4], 1) // length in 32-bit words - 1
	binary.BigEndian.PutUint32(pkt[4:8], 0) // SSRC
	_, _ = conn.WriteTo(pkt, remote)
}

// startRTCPKeepaliveLoop periodically sends RTCP keepalives.
func (r *RTPRelay) startRTCPKeepaliveLoop() {
	if r == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.stop:
				return
			case <-ticker.C:
				r.sendFakeRTCP()
			}
		}
	}()
}

// StartPCAP begins writing packets to a pcap file.
func (r *RTPRelay) StartPCAP(f osFile) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.pcap = &pcapWriter{file: f}
	// Write the pcap global header.
	if f != nil {
		_, _ = f.Write(pcapGlobalHeader())
	}
	r.mu.Unlock()
}

// StopPCAP stops writing packets.
func (r *RTPRelay) StopPCAP() {
	if r == nil {
		return
	}
	r.mu.Lock()
	p := r.pcap
	r.pcap = nil
	r.mu.Unlock()
	if p != nil && p.file != nil {
		_ = p.file.Close()
	}
}

// writePCAPPacket writes one packet to the pcap file.
func (r *RTPRelay) writePCAPPacket(pkt []byte) {
	if r == nil {
		return
	}
	r.mu.RLock()
	p := r.pcap
	r.mu.RUnlock()
	if p == nil || p.file == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = p.file.Write(pcapPacketHeader(len(pkt)))
	_, _ = p.file.Write(pkt)
}

// pcapGlobalHeader returns the pcap file global header.
func pcapGlobalHeader() []byte {
	h := make([]byte, 24)
	binary.LittleEndian.PutUint32(h[0:4], 0xa1b2c3d4) // magic
	binary.LittleEndian.PutUint16(h[4:6], 2)          // version major
	binary.LittleEndian.PutUint16(h[6:8], 4)          // version minor
	binary.LittleEndian.PutUint32(h[16:20], 0)        // network: loopback
	return h
}

// pcapPacketHeader returns a pcap per-packet header.
func pcapPacketHeader(length int) []byte {
	h := make([]byte, 16)
	now := time.Now()
	binary.LittleEndian.PutUint32(h[0:4], uint32(now.Unix()))
	binary.LittleEndian.PutUint32(h[4:8], uint32(now.Nanosecond()/1000))
	binary.LittleEndian.PutUint32(h[8:12], uint32(length))
	binary.LittleEndian.PutUint32(h[12:16], uint32(length))
	return h
}
