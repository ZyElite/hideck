package ipsec

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"
)

func (t *Socks5Transport) Start() error {
	t.lifecycle.Lock()
	defer t.lifecycle.Unlock()
	if t.stopped {
		return errors.New("SOCKS5 transport already stopped")
	}
	if t.started {
		return t.startErr
	}
	if t.tcpConn == nil || t.udpConn == nil {
		return errors.New("SOCKS5 transport is not initialized")
	}
	t.started = true
	t.wg.Add(3)
	go t.readLoop()
	go t.logStatsLoop()
	go t.tcpKeepalive()
	return t.startErr
}

func (t *Socks5Transport) Stop() {
	t.lifecycle.Lock()
	if t.stopped {
		t.lifecycle.Unlock()
		return
	}
	t.stopped = true
	t.cancel()
	if t.udpConn != nil {
		_ = t.udpConn.Close()
	}
	if t.tcpConn != nil {
		_ = t.tcpConn.Close()
	}
	t.lifecycle.Unlock()
	t.wg.Wait()
	close(t.ikeChan)
	close(t.espChan)
	close(t.netEvents)
}

func (t *Socks5Transport) SendIKE(packet []byte) error {
	if t.RemotePort() == 4500 {
		marked := make([]byte, 4, len(packet)+4)
		packet = append(marked, packet...)
	}
	return t.sendUDP(packet)
}

func (t *Socks5Transport) SendESP(packet []byte) error { return t.sendUDP(packet) }

func (t *Socks5Transport) SendNATKeepalive() error { return t.sendUDP([]byte{0xff}) }

func (t *Socks5Transport) sendUDP(data []byte) error {
	if t.udpConn == nil || t.relayAddr == nil {
		return errors.New("SOCKS5 UDP association is not initialized")
	}
	t.remoteMu.RLock()
	destination := &net.UDPAddr{IP: append(net.IP(nil), t.remoteIP...), Port: t.remotePort}
	t.remoteMu.RUnlock()
	datagram := EncodeSocks5UDPDatagram(destination, data)
	written, err := t.udpConn.WriteToUDP(datagram, t.relayAddr)
	if err != nil {
		return fmt.Errorf("send SOCKS5 UDP datagram: %w", err)
	}
	if written != len(datagram) {
		return io.ErrShortWrite
	}
	return nil
}

func (t *Socks5Transport) readLoop() {
	defer t.wg.Done()
	buffer := make([]byte, 65535)
	for {
		_ = t.udpConn.SetReadDeadline(time.Now().Add(5 * time.Second))
		length, _, err := t.udpConn.ReadFromUDP(buffer)
		if err != nil {
			if t.ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			logWarn("SOCKS5 UDP receive failed: " + err.Error())
			continue
		}
		t.processSocks5Datagram(buffer[:length])
	}
}

func (t *Socks5Transport) processSocks5Datagram(raw []byte) {
	atomic.AddUint64(&t.udpReadTotal, 1)
	atomic.StoreUint64(&t.lastUDPReadLen, uint64(len(raw)))
	datagram, err := DecodeSocks5UDPDatagram(raw)
	if err != nil {
		atomic.AddUint64(&t.udpDecodeErrorTotal, 1)
		logWarn("invalid SOCKS5 UDP datagram: " + err.Error())
		return
	}
	if datagram.Frag != 0 {
		atomic.AddUint64(&t.udpFragDropTotal, 1)
		return
	}
	payload := datagram.Data
	if len(payload) == 0 || (len(payload) == 1 && payload[0] == 0xff) {
		atomic.AddUint64(&t.natKeepaliveDrop, 1)
		return
	}
	if ike, ok := parseIKEPayload(payload, cap(payload)); ok {
		t.deliverSocks5Packet(ike, packetDelivery{
			channel: t.ikeChan, received: &t.receivedIKE, dropped: &t.droppedIKE, label: "IKE",
		})
		return
	}
	esp := stripNonESPMarker(payload)
	if len(esp) == 0 {
		return
	}
	atomic.StoreUint64(&t.lastESPReadLen, uint64(len(esp)))
	t.deliverSocks5Packet(esp, packetDelivery{
		channel: t.espChan, received: &t.receivedESP, dropped: &t.droppedESP, label: "ESP",
	})
}

func (t *Socks5Transport) deliverSocks5Packet(packet []byte, delivery packetDelivery) {
	copyPacket := append([]byte(nil), packet...)
	atomic.AddUint64(delivery.received, 1)
	select {
	case delivery.channel <- copyPacket:
	default:
		atomic.AddUint64(delivery.dropped, 1)
		logWarn(delivery.label + " packet dropped from SOCKS5 queue")
	}
}

func (t *Socks5Transport) logStatsLoop() {
	defer t.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			stats := t.SnapshotStats()
			logDebug(fmt.Sprintf("SOCKS5 stats: %+v", stats))
		}
	}
}

func (t *Socks5Transport) tcpKeepalive() {
	defer t.wg.Done()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	buffer := make([]byte, 1)
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
		}
		_ = t.tcpConn.SetReadDeadline(time.Now().Add(time.Second))
		_, err := t.tcpConn.Read(buffer)
		if err == nil {
			continue
		}
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			continue
		}
		if t.ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
			return
		}
		t.reportTCPFailure(err)
		return
	}
}

func (t *Socks5Transport) reportTCPFailure(err error) {
	reason := "SOCKS5 TCP control channel failed: " + err.Error()
	logWarn(reason)
	select {
	case t.netEvents <- NetEvent{Type: EventNetworkDown, Reason: reason}:
	default:
	}
}
