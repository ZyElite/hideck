package ipsec3gpp

import "sync"

type ReplayStats struct {
	Accepted  uint64
	Duplicate uint64
	TooOld    uint64
}

type ReplayWindow struct {
	mu          sync.Mutex
	size        uint32
	initialized bool
	highest     uint32
	bitmap      uint64
	stats       ReplayStats
}

func NewReplayWindow(size int) *ReplayWindow {
	if size <= 0 || size > 64 {
		size = 32
	}
	return &ReplayWindow{size: uint32(size)}
}

func (window *ReplayWindow) Accept(sequence uint32) bool {
	window.mu.Lock()
	defer window.mu.Unlock()
	if sequence == 0 {
		window.stats.TooOld++
		return false
	}
	if !window.initialized {
		window.initialized = true
		window.highest = sequence
		window.bitmap = 1
		window.stats.Accepted++
		return true
	}
	if sequence <= window.highest {
		return window.acceptOlder(sequence)
	}
	window.advance(sequence)
	window.stats.Accepted++
	return true
}

func (window *ReplayWindow) acceptOlder(sequence uint32) bool {
	difference := window.highest - sequence
	if difference >= window.size {
		window.stats.TooOld++
		return false
	}
	bit := uint64(1) << difference
	if window.bitmap&bit != 0 {
		window.stats.Duplicate++
		return false
	}
	window.bitmap |= bit
	window.stats.Accepted++
	return true
}

func (window *ReplayWindow) advance(sequence uint32) {
	difference := sequence - window.highest
	if difference < window.size {
		window.bitmap = (window.bitmap << difference) | 1
	} else {
		window.bitmap = 1
	}
	window.highest = sequence
}

func (window *ReplayWindow) Snapshot() ReplayStats {
	window.mu.Lock()
	defer window.mu.Unlock()
	return window.stats
}
