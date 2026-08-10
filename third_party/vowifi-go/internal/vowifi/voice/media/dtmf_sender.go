package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

func normalizeDTMFDuration(duration time.Duration, clockRate int) (int, uint16, error) {
	if duration < dtmfMinimum {
		duration = dtmfMinimum
	}
	maxDuration := time.Duration(dtmfMaximumDurationUnits) * time.Second / time.Duration(clockRate)
	if duration > maxDuration {
		return 0, 0, fmt.Errorf("media: DTMF duration %s exceeds RFC4733 field capacity", duration)
	}
	steps := int(duration / dtmfPacketInterval)
	if duration%dtmfPacketInterval != 0 {
		steps++
	}
	finalDuration := dtmfDurationUnits(steps, clockRate)
	if finalDuration > dtmfMaximumDurationUnits {
		return 0, 0, fmt.Errorf("media: DTMF duration %s exceeds RFC4733 field capacity after packetization", duration)
	}
	return steps, uint16(finalDuration), nil
}

func dtmfDurationUnits(step, clockRate int) uint64 {
	return uint64(step) * uint64(clockRate) * uint64(dtmfPacketInterval) / uint64(time.Second)
}

func (r *RTPRelay) dtmfEventTimestampLocked() uint32 {
	if !r.dtmfSourceObserved || r.dtmfLastRTPPacketAt.IsZero() {
		return r.dtmfTimestamp
	}
	elapsed := time.Since(r.dtmfLastRTPPacketAt)
	if elapsed < 0 {
		elapsed = 0
	}
	whole := uint64(elapsed/time.Second) * uint64(r.dtmfClockRate)
	fraction := uint64(elapsed%time.Second) * uint64(r.dtmfClockRate) / uint64(time.Second)
	return r.dtmfTimestamp + uint32(whole+fraction)
}

func (r *RTPRelay) startDTMFEvent(plan dtmfSendPlan) dtmfSendPlan {
	r.dtmfWriteMu.Lock()
	r.dtmfMu.Lock()
	plan.timestamp = r.dtmfEventTimestampLocked()
	plan.ssrc = r.dtmfSSRC
	eventDuration := time.Duration(plan.finalDuration) * time.Second / time.Duration(plan.clockRate)
	plan.eventEndAt = time.Now().Add(eventDuration)
	r.dtmfMu.Unlock()
	r.dtmfWriteMu.Unlock()
	return plan
}

func (r *RTPRelay) finishDTMFSend(plan dtmfSendPlan) {
	r.dtmfMu.Lock()
	r.dtmfTimestamp = plan.timestamp + uint32(plan.finalDuration)
	if r.dtmfSourceObserved {
		r.dtmfLastRTPPacketAt = plan.eventEndAt
	}
	r.dtmfSending = false
	r.dtmfRewritePending = r.dtmfSourceObserved
	r.dtmfMu.Unlock()
}

func (r *RTPRelay) sendDTMFEvent(plan dtmfSendPlan) error {
	for step := 1; step <= plan.steps; step++ {
		packet := dtmfPacket{
			plan: plan, duration: uint16(dtmfDurationUnits(step, plan.clockRate)), marker: step == 1,
		}
		if err := r.writeDTMFPacket(packet); err != nil {
			return err
		}
		if err := r.waitDTMFInterval(); err != nil {
			return err
		}
	}
	for repeat := 0; repeat < dtmfEndPacketCount; repeat++ {
		if err := r.writeDTMFPacket(dtmfPacket{plan: plan, duration: plan.finalDuration, end: true}); err != nil {
			return err
		}
		if repeat+1 < dtmfEndPacketCount {
			if err := r.waitDTMFInterval(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *RTPRelay) waitDTMFInterval() error {
	timer := time.NewTimer(dtmfPacketInterval)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-r.stopCh:
		return errors.New("media: RTP relay stopped during DTMF event")
	}
}

func (r *RTPRelay) writeDTMFPacket(spec dtmfPacket) error {
	r.dtmfWriteMu.Lock()
	r.dtmfMu.Lock()
	sequence := r.dtmfSequence
	r.dtmfMu.Unlock()
	packet := buildDTMFPacket(spec, sequence)
	err := writePacket(r.connIMS, packet, spec.plan.remote)
	if err == nil {
		r.dtmfMu.Lock()
		r.dtmfSequence++
		r.dtmfMu.Unlock()
	}
	r.dtmfWriteMu.Unlock()
	if err != nil {
		return fmt.Errorf("media: send RFC4733 event: %w", err)
	}
	r.writePCAPPacket(packet, pcapDirectionLANToIMS)
	atomic.AddUint64(&r.bytesLANToIMS, uint64(len(packet)))
	atomic.CompareAndSwapUint32(&r.lanFirstPacket, 0, 1)
	if monitor := r.monitorSnapshot(); monitor != nil {
		monitor.UpdateLAN()
	}
	return nil
}

func buildDTMFPacket(spec dtmfPacket, sequence uint16) []byte {
	packet := make([]byte, 16)
	packet[0] = 0x80
	packet[1] = byte(spec.plan.payloadType)
	if spec.marker {
		packet[1] |= 0x80
	}
	binary.BigEndian.PutUint16(packet[2:4], sequence)
	binary.BigEndian.PutUint32(packet[4:8], spec.plan.timestamp)
	binary.BigEndian.PutUint32(packet[8:12], spec.plan.ssrc)
	packet[12] = spec.plan.event
	packet[13] = dtmfVolume
	if spec.end {
		packet[13] |= 0x80
	}
	binary.BigEndian.PutUint16(packet[14:16], spec.duration)
	return packet
}
