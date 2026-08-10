package media

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

const (
	dtmfPacketInterval = 20 * time.Millisecond
	dtmfMinimum        = 40 * time.Millisecond
	dtmfClockRate      = 8000
	dtmfEndRepeats     = 3
	dtmfVolume         = 10
)

var dtmfEvents = map[rune]byte{
	'0': 0, '1': 1, '2': 2, '3': 3, '4': 4, '5': 5, '6': 6, '7': 7,
	'8': 8, '9': 9, '*': 10, '#': 11, 'A': 12, 'B': 13, 'C': 14, 'D': 15,
}

func (r *RTPRelay) seedDTMF() {
	seed := make([]byte, 10)
	if _, err := rand.Read(seed); err != nil {
		now := uint64(time.Now().UnixNano())
		binary.BigEndian.PutUint64(seed[:8], now)
	}
	r.dtmfSequence = binary.BigEndian.Uint16(seed[:2])
	r.dtmfTimestamp = binary.BigEndian.Uint32(seed[2:6])
	r.dtmfSSRC = binary.BigEndian.Uint32(seed[6:10])
}

// SetDTMFPayloadType stores the negotiated IMS telephone-event payload type.
func (r *RTPRelay) SetDTMFPayloadType(payloadType int) error {
	if r == nil {
		return errors.New("media: nil relay")
	}
	if payloadType < 0 || payloadType > 127 {
		return fmt.Errorf("media: invalid DTMF payload type %d", payloadType)
	}
	r.dtmfMu.Lock()
	r.dtmfPayloadType = payloadType
	r.dtmfMu.Unlock()
	return nil
}

// SendDTMF sends one RFC 4733 event to the negotiated IMS RTP peer.
func (r *RTPRelay) SendDTMF(digit rune, duration time.Duration) error {
	if r == nil {
		return errors.New("media: nil relay")
	}
	event, exists := dtmfEvents[digit]
	if !exists {
		return fmt.Errorf("media: unsupported DTMF digit %q", digit)
	}
	if duration < dtmfMinimum {
		duration = dtmfMinimum
	}
	maxDuration := time.Duration(^uint16(0)) * time.Second / dtmfClockRate
	if duration > maxDuration {
		return fmt.Errorf("media: DTMF duration %s exceeds RFC4733 field capacity", duration)
	}
	r.dtmfMu.Lock()
	defer r.dtmfMu.Unlock()
	if r.dtmfPayloadType < 0 {
		return errors.New("media: telephone-event payload type was not negotiated")
	}
	remote := r.remoteAddr.Load()
	if r.connIMS == nil || remote == nil {
		return errors.New("media: IMS RTP destination is unavailable")
	}
	return r.sendDTMFLocked(remote, event, duration)
}

func (r *RTPRelay) sendDTMFLocked(remote *net.UDPAddr, event byte, duration time.Duration) error {
	steps := int((duration + dtmfPacketInterval - 1) / dtmfPacketInterval)
	for step := 1; step <= steps; step++ {
		eventDuration := uint16(step * dtmfClockRate / int(time.Second/dtmfPacketInterval))
		if err := r.writeDTMFPacket(remote, event, eventDuration, step == 1, false); err != nil {
			return err
		}
		if step < steps {
			time.Sleep(dtmfPacketInterval)
		}
	}
	finalDuration := uint16(steps * dtmfClockRate / int(time.Second/dtmfPacketInterval))
	for repeat := 0; repeat < dtmfEndRepeats; repeat++ {
		if err := r.writeDTMFPacket(remote, event, finalDuration, false, true); err != nil {
			return err
		}
	}
	r.dtmfTimestamp += uint32(finalDuration)
	return nil
}

func (r *RTPRelay) writeDTMFPacket(
	remote *net.UDPAddr,
	event byte,
	duration uint16,
	marker, end bool,
) error {
	packet := make([]byte, 16)
	packet[0] = 0x80
	packet[1] = byte(r.dtmfPayloadType)
	if marker {
		packet[1] |= 0x80
	}
	binary.BigEndian.PutUint16(packet[2:4], r.dtmfSequence)
	binary.BigEndian.PutUint32(packet[4:8], r.dtmfTimestamp)
	binary.BigEndian.PutUint32(packet[8:12], r.dtmfSSRC)
	packet[12] = event
	packet[13] = dtmfVolume
	if end {
		packet[13] |= 0x80
	}
	binary.BigEndian.PutUint16(packet[14:16], duration)
	if err := writePacket(r.connIMS, packet, remote); err != nil {
		return fmt.Errorf("media: send RFC4733 event: %w", err)
	}
	r.writePCAPPacket(packet, pcapDirectionLANToIMS)
	r.dtmfSequence++
	return nil
}
