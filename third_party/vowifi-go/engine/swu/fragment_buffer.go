package swu

import (
	"errors"
	"fmt"
	"sync"
)

// fragmentBuffer reassembles IKEv2 fragmented messages (RFC 7383). Each IKE
// message identifier has its own fragment set; fragments are numbered 1..N.
type fragmentBuffer struct {
	mu        sync.Mutex
	fragments map[uint32]*fragmentSet
}

// fragmentSet holds the fragments received for a single IKE message id.
type fragmentSet struct {
	total uint16            // total fragment count declared by the sender
	frags map[uint16][]byte // fragment number → payload bytes
	size  int               // accumulated payload bytes (overflow guard)
}

// maxFragmentedMessageSize bounds a reassembled IKE message (the decompiled
// implementation rejects a running size >= 0x10001).
const maxFragmentedMessageSize = 0x10000

// newFragmentBuffer constructs an empty fragment buffer.
func newFragmentBuffer() *fragmentBuffer {
	return &fragmentBuffer{fragments: make(map[uint32]*fragmentSet)}
}

// addFragment stores a fragment for the given IKE message id. It returns true
// when the message is complete (all fragments received). A fragment total
// larger than the previously recorded total resets the set; a mismatched
// (smaller) total, a duplicate fragment or an oversized message is rejected.
func (f *fragmentBuffer) addFragment(messageID uint32, fragNum, total uint16, data []byte) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	set, ok := f.fragments[messageID]
	if !ok {
		set = &fragmentSet{total: total, frags: make(map[uint16][]byte)}
		f.fragments[messageID] = set
	}
	if set.total < total {
		// The sender declared a larger total than we recorded: restart.
		set.total = total
		set.frags = make(map[uint16][]byte)
		set.size = 0
	} else if total != set.total {
		return false, fmt.Errorf("fragment total mismatch: have %d, got %d", set.total, total)
	}

	if _, dup := set.frags[fragNum]; dup {
		return false, nil
	}

	if set.size+len(data) > maxFragmentedMessageSize {
		delete(f.fragments, messageID)
		return false, fmt.Errorf("fragmented message %d exceeds maximum size %d", messageID, maxFragmentedMessageSize)
	}

	set.frags[fragNum] = append([]byte{}, data...)
	set.size += len(data)

	return len(set.frags) == int(set.total), nil
}

// reassemble concatenates the fragments of a complete message in order and
// removes the set from the buffer. It errors if the message is unknown or
// incomplete.
func (f *fragmentBuffer) reassemble(messageID uint32) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	set, ok := f.fragments[messageID]
	if !ok {
		return nil, errors.New("no fragments for message")
	}

	out := make([]byte, 0, set.size)
	for i := uint16(1); i <= set.total; i++ {
		frag, ok := set.frags[i]
		if !ok {
			return nil, fmt.Errorf("missing fragment %d of %d for message %d", i, set.total, messageID)
		}
		out = append(out, frag...)
	}
	delete(f.fragments, messageID)
	return out, nil
}

// drop removes any partial fragment set for the message (e.g. on abort).
func (f *fragmentBuffer) drop(messageID uint32) {
	f.mu.Lock()
	delete(f.fragments, messageID)
	f.mu.Unlock()
}
