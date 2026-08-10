package media

import (
	"encoding/binary"
	"time"
)

func (r *RTPRelay) prepareLANRTPPacket(packet []byte) bool {
	if len(packet) < 12 || packet[0]&0xc0 != 0x80 {
		return true
	}
	sequence := binary.BigEndian.Uint16(packet[2:4])
	timestamp := binary.BigEndian.Uint32(packet[4:8])
	ssrc := binary.BigEndian.Uint32(packet[8:12])
	r.dtmfMu.Lock()
	defer r.dtmfMu.Unlock()
	if r.dtmfSending {
		return false
	}
	if !r.dtmfSourceObserved || r.dtmfSSRC != ssrc {
		r.observeDTMFSourceLocked(sequence, timestamp, ssrc)
	}
	if r.dtmfRewritePending {
		r.dtmfSequenceOffset = r.dtmfSequence - sequence
		r.dtmfRewritePending = false
	}
	outputSequence := sequence + r.dtmfSequenceOffset
	binary.BigEndian.PutUint16(packet[2:4], outputSequence)
	r.dtmfSequence = outputSequence + 1
	r.dtmfTimestamp = timestamp
	r.dtmfLastRTPPacketAt = time.Now()
	return true
}

func (r *RTPRelay) observeDTMFSourceLocked(sequence uint16, timestamp, ssrc uint32) {
	r.dtmfSourceObserved = true
	r.dtmfSequence = sequence
	r.dtmfTimestamp = timestamp
	r.dtmfSSRC = ssrc
	r.dtmfSeedErr = nil
	r.dtmfRewritePending = false
	r.dtmfSequenceOffset = 0
}
