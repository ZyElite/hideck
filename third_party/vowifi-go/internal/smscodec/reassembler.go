package smscodec

import (
	"bytes"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Fragment is one part of a concatenated (multipart) SMS.
//
// The original stores fragments as 64-byte records inside a
// map[string][]Fragment on the Reassembler; PartNo indexes the part within the
// concatenated message and Timestamp records when it arrived (used by
// Cleanup to expire stale groups).
type Fragment struct {
	PartNo    int
	Data      []byte
	Timestamp time.Time
}

// Reassembler reassembles concatenated SMS payloads (3GPP TS 23.040 9.2.3.24
// concatenated short messages, 8-bit and 16-bit reference forms).
type Reassembler struct {
	mu        sync.Mutex
	fragments map[string][]Fragment
}

// NewReassembler returns an empty reassembler.
func NewReassembler() *Reassembler {
	return &Reassembler{fragments: map[string][]Fragment{}}
}

// Add stores one part of a concatenated message.
//
// The group key identifies the concatenation: originating address plus the
// message reference and sequence number (the original builds
// fmt.Sprintf("%s_%d%d.%d (%s)", from, ref, seqNo, total, partNo)).
//
// It returns the fully reassembled payload with complete=true once every part
// of the group has been seen; duplicate part numbers are ignored.
func (r *Reassembler) Add(from string, ref, seqNo uint64, total, partNo int, data []byte, ts time.Time) ([]byte, bool) {
	key := fragmentKey(from, ref, seqNo, total)

	r.mu.Lock()
	defer r.mu.Unlock()

	parts := r.fragments[key]
	for _, f := range parts {
		if f.PartNo == partNo {
			return nil, false // duplicate part
		}
	}
	parts = append(parts, Fragment{PartNo: partNo, Data: data, Timestamp: ts})
	if len(parts) != total {
		r.fragments[key] = parts
		return nil, false // still waiting for more parts
	}

	// All parts present: order by part number and concatenate.
	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNo < parts[j].PartNo })
	var buf bytes.Buffer
	for _, f := range parts {
		buf.Write(f.Data)
	}
	delete(r.fragments, key)
	return buf.Bytes(), true
}

// fragmentKey groups parts belonging to the same concatenated message.
func fragmentKey(from string, ref, seqNo uint64, total int) string {
	return fmt.Sprintf("%s_%d%d.%d (%d)", from, ref, seqNo, total, 0)
}

// Cleanup drops concatenation groups whose newest part is older than the
// given duration (avoids unbounded memory growth on lost parts).
func (r *Reassembler) Cleanup(olderThan time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	for key, parts := range r.fragments {
		// newest timestamp in the group
		var newest time.Time
		for _, f := range parts {
			if f.Timestamp.After(newest) {
				newest = f.Timestamp
			}
		}
		if newest.IsZero() || newest.Before(cutoff) {
			delete(r.fragments, key)
		}
	}
}
