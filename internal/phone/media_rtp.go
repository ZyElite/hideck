package phone

import (
	"errors"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

const (
	mediaPacketBuffer = 32
	jitterTick        = 20 * time.Millisecond
)

func (s *MediaSession) forwardBrowserRTP(track *webrtc.TrackRemote) {
	for {
		packet, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		endpoint, ok := s.endpoint()
		if !ok {
			continue
		}
		transcodeG711(packet.Payload, "PCMU", endpoint.Codec)
		packet.PayloadType = endpoint.PayloadType
		raw, err := packet.Marshal()
		if err != nil {
			continue
		}
		if _, err := s.rtpConn.WriteToUDP(raw, endpoint.Address); err == nil {
			s.toIMS.Add(1)
		}
	}
}

func (s *MediaSession) forwardIMSRTP() {
	packets := make(chan *rtp.Packet, mediaPacketBuffer)
	go s.readIMSRTP(packets)
	ticker := time.NewTicker(jitterTick)
	defer ticker.Stop()
	buffer := make(map[uint16]*rtp.Packet, mediaPacketBuffer)
	var expected uint16
	started := false
	for {
		select {
		case packet := <-packets:
			if !started {
				expected, started = packet.SequenceNumber, true
			}
			if len(buffer) >= mediaPacketBuffer {
				s.lost.Add(1)
				continue
			}
			buffer[packet.SequenceNumber] = packet
		case <-ticker.C:
			if !started || len(buffer) == 0 {
				continue
			}
			packet := buffer[expected]
			if packet == nil {
				s.lost.Add(1)
				expected++
				continue
			}
			delete(buffer, expected)
			expected++
			s.writeBrowserRTP(packet)
		case <-s.closed:
			return
		}
	}
}

func (s *MediaSession) writeBrowserRTP(packet *rtp.Packet) {
	endpoint, ok := s.endpoint()
	if ok {
		transcodeG711(packet.Payload, endpoint.Codec, "PCMU")
	}
	packet.PayloadType = pcmPayloadType
	if err := s.track.WriteRTP(packet); err == nil {
		s.fromIMS.Add(1)
	}
}

func (s *MediaSession) readIMSRTP(destination chan<- *rtp.Packet) {
	buffer := make([]byte, 2048)
	for {
		read, _, err := s.rtpConn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		packet := &rtp.Packet{}
		raw := append([]byte(nil), buffer[:read]...)
		if packet.Unmarshal(raw) != nil {
			continue
		}
		select {
		case destination <- packet:
		default:
			s.lost.Add(1)
		}
	}
}

func (s *MediaSession) endpoint() (rtpEndpoint, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.remote, s.attached
}

func (s *MediaSession) primeRelay() error {
	endpoint, ok := s.endpoint()
	if !ok {
		return errors.New("phone: media is not attached")
	}
	packet := &rtp.Packet{Header: rtp.Header{
		Version: 2, PayloadType: endpoint.PayloadType, SequenceNumber: 1,
		Timestamp: 160, SSRC: 0x564f4849,
	}, Payload: make([]byte, 160)}
	for index := range packet.Payload {
		packet.Payload[index] = 0xff
	}
	transcodeG711(packet.Payload, "PCMU", endpoint.Codec)
	raw, err := packet.Marshal()
	if err != nil {
		return err
	}
	_, err = s.rtpConn.WriteToUDP(raw, endpoint.Address)
	return err
}
