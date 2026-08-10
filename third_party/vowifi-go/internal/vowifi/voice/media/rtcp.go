package media

import (
	"net"
	"sync/atomic"
	"time"
)

var emptyReceiverReport = []byte{0x80, 0xc9, 0x00, 0x01, 0x12, 0x34, 0x56, 0x78}

func (r *RTPRelay) loopIMSRTCP() {
	r.readLoop(r.connIMSRTCP, func(packet []byte, source *net.UDPAddr) {
		r.handleIMSRTCPPacket(packet, source)
	})
}

func (r *RTPRelay) loopLANRTCP() {
	r.readLoop(r.connLANRTCP, func(packet []byte, source *net.UDPAddr) {
		r.handleLANRTCPPacket(packet, source)
	})
}

func (r *RTPRelay) handleIMSRTCPPacket(packet []byte, source *net.UDPAddr) {
	remote := r.remoteAddrRTCP.Load()
	if remote != nil && !remote.IP.Equal(source.IP) {
		return
	}
	client := r.clientAddrRTCP.Load()
	if client == nil {
		return
	}
	if err := writePacket(r.connLANRTCP, packet, client); err != nil {
		r.logWriteError("LAN RTCP", err)
		return
	}
	atomic.AddUint64(&r.bytesIMSRTCPToLAN, uint64(len(packet)))
	atomic.CompareAndSwapUint32(&r.imsRTCPFirstPacket, 0, 1)
}

func (r *RTPRelay) handleLANRTCPPacket(packet []byte, source *net.UDPAddr) {
	learnAddress(&r.clientAddrRTCP, source)
	remote := r.remoteAddrRTCP.Load()
	if remote == nil {
		return
	}
	if err := writePacket(r.connIMSRTCP, packet, remote); err != nil {
		r.logWriteError("IMS RTCP", err)
		return
	}
	atomic.AddUint64(&r.bytesLANRTCPToIMS, uint64(len(packet)))
	atomic.CompareAndSwapUint32(&r.lanRTCPFirstPacket, 0, 1)
}

func (r *RTPRelay) sendFakeRTCP() {
	if r == nil {
		return
	}
	remote := r.remoteAddrRTCP.Load()
	if remote == nil || r.connIMSRTCP == nil {
		return
	}
	if err := writePacket(r.connIMSRTCP, emptyReceiverReport, remote); err != nil {
		r.logWriteError("IMS RTCP keepalive", err)
	}
}

func (r *RTPRelay) startRTCPKeepaliveLoop() {
	if r == nil || r.isStopped() {
		return
	}
	r.rtcpMu.Lock()
	if r.isStopped() {
		r.rtcpMu.Unlock()
		return
	}
	r.rtcpWG.Add(1)
	r.rtcpKeepaliveTimer = time.AfterFunc(rtcpKeepalive, func() {
		defer r.rtcpWG.Done()
		r.runRTCPKeepalive()
	})
	r.rtcpMu.Unlock()
}

func (r *RTPRelay) runRTCPKeepalive() {
	if r.isStopped() {
		return
	}
	lastOutbound := int64(0)
	if monitor := r.monitorSnapshot(); monitor != nil {
		lastOutbound = monitor.LastLANToIMS.Load()
	}
	if lastOutbound == 0 || time.Since(time.Unix(0, lastOutbound)) >= rtcpKeepalive {
		r.sendFakeRTCP()
	}
	r.startRTCPKeepaliveLoop()
}

func (r *RTPRelay) stopRTCPKeepalive() {
	r.rtcpMu.Lock()
	if r.rtcpKeepaliveTimer != nil {
		if r.rtcpKeepaliveTimer.Stop() {
			r.rtcpWG.Done()
		}
		r.rtcpKeepaliveTimer = nil
	}
	r.rtcpMu.Unlock()
	r.rtcpWG.Wait()
}
