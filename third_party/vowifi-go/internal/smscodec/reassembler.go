package smscodec

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const reassemblerMaxGroups = 64

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
	completed map[string]time.Time
}

// NewReassembler returns an empty reassembler.
func NewReassembler() *Reassembler {
	return &Reassembler{
		fragments: make(map[string][]Fragment),
		completed: make(map[string]time.Time),
	}
}

// Add stores one part of a concatenated message.
//
// The group key identifies the concatenation by originating address, the
// 8-bit/16-bit reference components and the declared part count.
//
// It returns the fully reassembled payload with complete=true once every part
// of the group has been seen. Duplicate parts, including repeats received
// after completion, are ignored until Cleanup expires the completed key.
func (r *Reassembler) Add(from string, ref, seqNo uint64, total, partNo int, data []byte, ts time.Time) ([]byte, bool) {
	if r == nil || total <= 0 || partNo <= 0 || partNo > total {
		return nil, false
	}
	if ts.IsZero() {
		ts = time.Now()
	}
	key := fragmentKey(from, ref, seqNo, total)

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, alreadyCompleted := r.completed[key]; alreadyCompleted {
		return nil, false
	}

	parts := r.fragments[key]
	if len(parts) == 0 && len(r.fragments)+len(r.completed) >= reassemblerMaxGroups {
		r.evictOldestLocked()
	}
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
	r.completed[key] = ts
	return buf.Bytes(), true
}

func (r *Reassembler) evictOldestLocked() {
	oldestKey := ""
	oldestAt := time.Time{}
	completed := false
	for key, at := range r.completed {
		if oldestKey == "" || at.Before(oldestAt) {
			oldestKey, oldestAt, completed = key, at, true
		}
	}
	for key, parts := range r.fragments {
		newest := time.Time{}
		for _, part := range parts {
			if part.Timestamp.After(newest) {
				newest = part.Timestamp
			}
		}
		if oldestKey == "" || newest.Before(oldestAt) {
			oldestKey, oldestAt, completed = key, newest, false
		}
	}
	if completed {
		delete(r.completed, oldestKey)
	} else {
		delete(r.fragments, oldestKey)
	}
}

// fragmentKey groups parts belonging to the same concatenated message.
func fragmentKey(from string, ref, seqNo uint64, total int) string {
	return fmt.Sprintf("%s|%d|%d|%d", strings.TrimSpace(from), ref, seqNo, total)
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
	for key, completedAt := range r.completed {
		if completedAt.Before(cutoff) {
			delete(r.completed, key)
		}
	}
}
