package phone

import (
	"errors"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

const (
	mediaPacketBuffer      = 32
	jitterTick             = 20 * time.Millisecond
	browserClockRate       = 8000
	audioFramesPerSecond   = 50
	browserSamplesPerFrame = browserClockRate / audioFramesPerSecond
)

func (s *MediaSession) forwardBrowserRTP(track *webrtc.TrackRemote) {
	for {
		packet, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		endpoint, codec, ok := s.endpoint()
		if !ok {
			continue
		}
		s.recordMixedFrame(mixToIMS, packet.Payload)
		packet.Payload, err = browserPayloadForIMS(packet.Payload, endpoint, codec)
		if err != nil {
			s.lost.Add(1)
			continue
		}
		packet.PayloadType = endpoint.PayloadType
		packet.Timestamp = scaleTimestamp(packet.Timestamp, browserClockRate, endpoint.ClockRate)
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
	endpoint, codec, ok := s.endpoint()
	if !ok {
		return
	}
	payload, err := imsPayloadForBrowser(packet.Payload, endpoint, codec)
	if err != nil {
		s.lost.Add(1)
		return
	}
	packet.Payload = payload
	s.recordMixedFrame(mixFromIMS, payload)
	packet.PayloadType = pcmPayloadType
	packet.Timestamp = scaleTimestamp(packet.Timestamp, endpoint.ClockRate, browserClockRate)
	if err := s.track.WriteRTP(packet); err == nil {
		s.fromIMS.Add(1)
	}
}

func (s *MediaSession) recordMixedFrame(direction mixDirection, payload []byte) {
	s.mu.RLock()
	recorder := s.recorder
	s.mu.RUnlock()
	if recorder != nil {
		recorder.Add(direction, payload)
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

func (s *MediaSession) endpoint() (rtpEndpoint, RealtimeCodec, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.remote, s.realtimeCodec, s.attached
}

func (s *MediaSession) primeRelay() error {
	endpoint, _, ok := s.endpoint()
	if !ok {
		return errors.New("phone: media is not attached")
	}
	return s.writeSilentRTP(1, uint32(endpoint.ClockRate/audioFramesPerSecond))
}

func (s *MediaSession) forwardSilentRTP() {
	ticker := time.NewTicker(jitterTick)
	defer ticker.Stop()
	sequence := uint16(1)
	endpoint, _, ok := s.endpoint()
	if !ok {
		return
	}
	timestamp := uint32(endpoint.ClockRate / audioFramesPerSecond)
	for {
		select {
		case <-ticker.C:
			endpoint, _, ok = s.endpoint()
			if !ok {
				continue
			}
			sequence++
			timestamp += uint32(endpoint.ClockRate / audioFramesPerSecond)
			if err := s.writeSilentRTP(sequence, timestamp); err != nil {
				s.lost.Add(1)
			}
		case <-s.closed:
			return
		}
	}
}

func (s *MediaSession) writeSilentRTP(sequence uint16, timestamp uint32) error {
	endpoint, codec, ok := s.endpoint()
	if !ok {
		return errors.New("phone: media is not attached")
	}
	payload := make([]byte, browserSamplesPerFrame)
	for index := range payload {
		payload[index] = 0xff
	}
	s.recordMixedFrame(mixToIMS, payload)
	payload, err := browserPayloadForIMS(payload, endpoint, codec)
	if err != nil {
		return err
	}
	packet := &rtp.Packet{Header: rtp.Header{
		Version: 2, PayloadType: endpoint.PayloadType, SequenceNumber: sequence,
		Timestamp: timestamp, SSRC: 0x564f4849,
	}, Payload: payload}
	raw, err := packet.Marshal()
	if err != nil {
		return err
	}
	_, err = s.rtpConn.WriteToUDP(raw, endpoint.Address)
	if err == nil {
		s.toIMS.Add(1)
	}
	return err
}

func browserPayloadForIMS(payload []byte, endpoint rtpEndpoint, codec RealtimeCodec) ([]byte, error) {
	if len(payload) != browserSamplesPerFrame {
		return nil, errors.New("phone: browser PCMU packet must contain one 20ms frame")
	}
	if endpoint.Codec == "PCMU" {
		return payload, nil
	}
	if endpoint.Codec == "PCMA" {
		transcodeG711(payload, "PCMU", "PCMA")
		return payload, nil
	}
	if codec == nil {
		return nil, errors.New("phone: realtime encoder is unavailable")
	}
	pcm, err := resamplePCM(decodePCMU(payload), browserClockRate, codec.SampleRate())
	if err != nil {
		return nil, err
	}
	return codec.Encode(pcm)
}

func imsPayloadForBrowser(payload []byte, endpoint rtpEndpoint, codec RealtimeCodec) ([]byte, error) {
	if endpoint.Codec == "PCMU" {
		if len(payload) != browserSamplesPerFrame {
			return nil, errors.New("phone: IMS PCMU packet must contain one 20ms frame")
		}
		return payload, nil
	}
	if endpoint.Codec == "PCMA" {
		if len(payload) != browserSamplesPerFrame {
			return nil, errors.New("phone: IMS PCMA packet must contain one 20ms frame")
		}
		transcodeG711(payload, "PCMA", "PCMU")
		return payload, nil
	}
	if codec == nil {
		return nil, errors.New("phone: realtime decoder is unavailable")
	}
	pcm, err := codec.Decode(payload)
	if err != nil {
		return nil, err
	}
	pcm, err = resamplePCM(pcm, codec.SampleRate(), browserClockRate)
	if err != nil {
		return nil, err
	}
	return encodePCMU(pcm), nil
}

func scaleTimestamp(timestamp uint32, fromRate, toRate int) uint32 {
	if fromRate <= 0 || toRate <= 0 || fromRate == toRate {
		return timestamp
	}
	return uint32(uint64(timestamp) * uint64(toRate) / uint64(fromRate))
}
