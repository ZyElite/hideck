package ipsec3gpp

import "sync"

// ReplayWindow implements the RFC 4303 anti-replay window.
type ReplayWindow struct {
	mu      sync.Mutex
	window  uint64
	highest uint32
	size    int
}

func NewReplayWindow(size int) *ReplayWindow {
	if size <= 0 || size > 64 {
		size = 32
	}
	return &ReplayWindow{size: size}
}

func (w *ReplayWindow) Accept(sequence uint32) bool {
	if sequence == 0 {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.highest == 0 {
		w.highest = sequence
		w.window = 1
		return true
	}
	if sequence > w.highest {
		shift := uint64(sequence - w.highest)
		if shift >= uint64(w.size) {
			w.window = 1
		} else {
			w.window = (w.window << shift) | 1
		}
		w.highest = sequence
		return true
	}
	shift := uint64(w.highest - sequence)
	if shift >= uint64(w.size) {
		return false
	}
	bit := uint64(1) << shift
	if w.window&bit != 0 {
		return false
	}
	w.window |= bit
	return true
}

func (w *ReplayWindow) Snapshot() (uint32, uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.highest, w.window
}
