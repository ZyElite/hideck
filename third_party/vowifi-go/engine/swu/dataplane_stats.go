package swu

import "sync/atomic"

// InnerPacketSnapshot is the original public userspace data-plane snapshot.
type InnerPacketSnapshot struct {
	OutboundPackets     uint64
	InboundPackets      uint64
	SAMisses            uint64
	EncapsulationErrors uint64
	DecapsulationErrors uint64
	SendErrors          uint64
	LastOutboundLen     uint64
	LastInboundLen      uint64
}

type dataPlaneRuntimeStats struct {
	tunRead        atomic.Uint64
	espSend        atomic.Uint64
	espSendError   atomic.Uint64
	espOutSAMiss   atomic.Uint64
	espEncapError  atomic.Uint64
	espIn          atomic.Uint64
	espInSAMiss    atomic.Uint64
	espDecapError  atomic.Uint64
	tunWrite       atomic.Uint64
	tunWriteError  atomic.Uint64
	lastInSPI      atomic.Uint32
	lastPlainLen   atomic.Uint64
	lastTunReadLen atomic.Uint64
}

type dataPlaneStatsSnapshot struct {
	tunRead, espSend, espSendError       uint64
	espOutSAMiss, espEncapError, espIn   uint64
	espInSAMiss, espDecapError, tunWrite uint64
	tunWriteError                        uint64
	lastInSPI                            uint32
	lastPlainLen, lastTunReadLen         uint64
	innerPacket                          InnerPacketSnapshot
}

func (s *dataPlaneRuntimeStats) snapshot() dataPlaneStatsSnapshot {
	snapshot := dataPlaneStatsSnapshot{
		tunRead: s.tunRead.Load(), espSend: s.espSend.Load(),
		espSendError: s.espSendError.Load(), espOutSAMiss: s.espOutSAMiss.Load(),
		espEncapError: s.espEncapError.Load(), espIn: s.espIn.Load(),
		espInSAMiss: s.espInSAMiss.Load(), espDecapError: s.espDecapError.Load(),
		tunWrite: s.tunWrite.Load(), tunWriteError: s.tunWriteError.Load(),
		lastInSPI: s.lastInSPI.Load(), lastPlainLen: s.lastPlainLen.Load(),
		lastTunReadLen: s.lastTunReadLen.Load(),
	}
	snapshot.innerPacket = projectInnerPacketSnapshot(snapshot)
	return snapshot
}

func (s *dataPlaneRuntimeStats) innerPacketSnapshot() InnerPacketSnapshot {
	return s.snapshot().innerPacket
}

func projectInnerPacketSnapshot(s dataPlaneStatsSnapshot) InnerPacketSnapshot {
	return InnerPacketSnapshot{
		OutboundPackets:     s.espSend,
		InboundPackets:      s.tunWrite,
		SAMisses:            s.espOutSAMiss + s.espInSAMiss,
		EncapsulationErrors: s.espEncapError,
		DecapsulationErrors: s.espDecapError,
		SendErrors:          s.espSendError,
		LastOutboundLen:     s.lastTunReadLen,
		LastInboundLen:      s.lastPlainLen,
	}
}
