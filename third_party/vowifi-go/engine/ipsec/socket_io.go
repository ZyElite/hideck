package ipsec

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
)

type packetDelivery struct {
	channel  chan<- []byte
	received *uint64
	dropped  *uint64
	label    string
}

func (s *SocketManager) readLoop() {
	defer s.wg.Done()
	buffer := make([]byte, directReadBufSize)
	for {
		length, source, err := s.Conn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		if !s.acceptSource(source) {
			continue
		}
		packet := append([]byte(nil), buffer[:length]...)
		s.dispatchDatagram(packet)
	}
}

func (s *SocketManager) acceptSource(source *net.UDPAddr) bool {
	s.remoteMu.Lock()
	defer s.remoteMu.Unlock()
	known, match := len(s.remoteIPs) == 0, -1
	for index, ip := range s.remoteIPs {
		if source.IP.Equal(ip) {
			known, match = true, index
			break
		}
	}
	if !known {
		return false
	}
	if match >= 0 && len(s.remoteIPs) > 1 {
		s.RemoteAddr.IP = append(net.IP(nil), source.IP...)
		s.remoteIPs = []net.IP{append(net.IP(nil), source.IP...)}
		s.remoteIdx = 0
		logDebug("locked ePDG endpoint to " + source.IP.String())
	}
	if s.RemoteAddr != nil && source.Port > 0 && source.Port != s.RemoteAddr.Port {
		s.reportPortChange(source.Port)
	}
	return true
}

func (s *SocketManager) reportPortChange(newPort int) {
	oldPort := s.RemoteAddr.Port
	s.RemoteAddr.Port = newPort
	reason := fmt.Sprintf("NAT port changed %d -> %d", oldPort, newPort)
	logInfo(reason)
	select {
	case s.NetEvents <- NetEvent{
		Type: EventNATPortChanged, OldPort: oldPort, NewPort: newPort, Reason: reason,
	}:
	default:
	}
}

func (s *SocketManager) dispatchDatagram(packet []byte) {
	if len(packet) == 1 && packet[0] == 0xff {
		return
	}
	if ike, ok := parseIKEPayload(packet, cap(packet)); ok {
		s.deliverPacket(ike, packetDelivery{
			channel: s.IKEChan, received: &s.receivedIKE, dropped: &s.droppedIKE, label: "IKE",
		})
		return
	}
	esp := stripNonESPMarker(packet)
	if len(esp) > 0 {
		s.deliverPacket(esp, packetDelivery{
			channel: s.ESPChan, received: &s.receivedESP, dropped: &s.droppedESP, label: "ESP",
		})
	}
}

func (s *SocketManager) deliverPacket(packet []byte, delivery packetDelivery) {
	select {
	case delivery.channel <- packet:
		atomic.AddUint64(delivery.received, 1)
	default:
		count := atomic.AddUint64(delivery.dropped, 1)
		if count == 1 || count%100 == 0 {
			logWarn(fmt.Sprintf("%s packet dropped (queue full): %d", delivery.label, count))
		}
	}
}

func stripNonESPMarker(packet []byte) []byte {
	if len(packet) >= 4 && packet[0] == 0 && packet[1] == 0 && packet[2] == 0 && packet[3] == 0 {
		return packet[4:]
	}
	return packet
}

func (s *SocketManager) SendIKE(packet []byte) error {
	destination, err := s.nextIKEDestination()
	if err != nil {
		return err
	}
	if destination.Port == 4500 {
		marked := make([]byte, 4, len(packet)+4)
		packet = append(marked, packet...)
	}
	return s.writeDatagram(packet, destination)
}

func (s *SocketManager) nextIKEDestination() (*net.UDPAddr, error) {
	s.remoteMu.Lock()
	defer s.remoteMu.Unlock()
	if s.RemoteAddr == nil {
		return nil, errors.New("remote UDP address is not configured")
	}
	if len(s.remoteIPs) > 1 {
		index := int(s.remoteIdx % uint32(len(s.remoteIPs)))
		s.remoteIdx++
		s.RemoteAddr.IP = append(net.IP(nil), s.remoteIPs[index]...)
		logDebug("sending IKE to " + s.RemoteAddr.IP.String())
	}
	copyAddr := *s.RemoteAddr
	copyAddr.IP = append(net.IP(nil), s.RemoteAddr.IP...)
	return &copyAddr, nil
}

func (s *SocketManager) SendESP(packet []byte) error {
	destination, err := s.remoteDestination()
	if err != nil {
		return err
	}
	return s.writeDatagram(packet, destination)
}

func (s *SocketManager) SendNATKeepalive() error {
	destination, err := s.remoteDestination()
	if err != nil {
		return err
	}
	return s.writeDatagram([]byte{0xff}, destination)
}

func (s *SocketManager) remoteDestination() (*net.UDPAddr, error) {
	s.remoteMu.Lock()
	defer s.remoteMu.Unlock()
	if s.RemoteAddr == nil {
		return nil, errors.New("remote UDP address is not configured")
	}
	copyAddr := *s.RemoteAddr
	copyAddr.IP = append(net.IP(nil), s.RemoteAddr.IP...)
	return &copyAddr, nil
}

func (s *SocketManager) writeDatagram(packet []byte, destination *net.UDPAddr) error {
	if s.Conn == nil {
		return errors.New("socket not created")
	}
	written, err := s.Conn.WriteToUDP(packet, destination)
	if err != nil {
		if errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection") {
			return err
		}
		return fmt.Errorf("send UDP datagram to %s: %w", destination, err)
	}
	if written != len(packet) {
		return io.ErrShortWrite
	}
	return nil
}
