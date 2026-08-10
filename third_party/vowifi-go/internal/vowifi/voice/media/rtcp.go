package media

import (
	"net"
	"sync/atomic"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

var emptyReceiverReport = []byte{0x80, 0xc9, 0x00, 0x01, 0x12, 0x34, 0x56, 0x78}

type rtcpPacketTrace struct {
	direction string
	size      int
	source    *net.UDPAddr
	target    *net.UDPAddr
}

func (r *RTPRelay) loopIMSRTCP() {
	r.readLoop(r.connIMSRTCP, time.Second, func(packet []byte, source *net.UDPAddr) {
		r.handleIMSRTCPPacket(packet, source)
	})
}

func (r *RTPRelay) loopLANRTCP() {
	r.readLoop(r.connLANRTCP, time.Second, func(packet []byte, source *net.UDPAddr) {
		r.handleLANRTCPPacket(packet, source)
	})
}

func (r *RTPRelay) handleIMSRTCPPacket(packet []byte, source *net.UDPAddr) {
	client := r.clientAddrRTCP.Load()
	if client != nil {
		if err := writePacket(r.connLANRTCP, packet, client); err != nil {
			r.logWriteError("LAN RTCP", err)
		} else if atomic.CompareAndSwapUint32(&r.imsRTCPFirstPacket, 0, 1) {
			r.logFirstRTCP(rtcpPacketTrace{
				direction: "IMS RTCP → LAN", size: len(packet), source: source, target: client,
			})
		}
	}
	atomic.AddUint64(&r.bytesIMSRTCPToLAN, uint64(len(packet)))
	if monitor := r.monitorSnapshot(); monitor != nil {
		monitor.UpdateIMS()
	}
}

func (r *RTPRelay) handleLANRTCPPacket(packet []byte, source *net.UDPAddr) {
	remote := r.remoteAddrRTCP.Load()
	if remote != nil {
		if err := writePacket(r.connIMSRTCP, packet, remote); err != nil {
			r.logWriteError("IMS RTCP", err)
		} else if atomic.CompareAndSwapUint32(&r.lanRTCPFirstPacket, 0, 1) {
			r.logFirstRTCP(rtcpPacketTrace{
				direction: "LAN RTCP → IMS", size: len(packet), source: source, target: remote,
			})
		}
	}
	atomic.AddUint64(&r.bytesLANRTCPToIMS, uint64(len(packet)))
	if monitor := r.monitorSnapshot(); monitor != nil {
		monitor.UpdateLAN()
	}
}

func (r *RTPRelay) logFirstRTCP(packet rtcpPacketTrace) {
	deviceID, traceID := r.logContext()
	logging.Debug("RTPRelay 首包确认: "+packet.direction,
		"device", deviceID, "trace", traceID, "bytes", packet.size,
		"source", packet.source, "target", packet.target)
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
	if r.rtcpKeepaliveTimer != nil && r.rtcpKeepaliveTimer.Stop() {
		r.rtcpWG.Done()
	}
	r.rtcpWG.Add(1)
	r.rtcpKeepaliveTimer = time.AfterFunc(rtcpKeepalive, func() {
		defer r.rtcpWG.Done()
		r.runRTCPKeepalive()
	})
	r.rtcpMu.Unlock()
}

func (r *RTPRelay) runRTCPKeepalive() {
	r.mu.RLock()
	active := r.active
	monitor := r.Monitor
	lastOutbound := int64(0)
	if monitor != nil {
		lastOutbound = monitor.LastLANToIMS.Load()
	}
	r.mu.RUnlock()
	if !active || r.isStopped() {
		return
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
