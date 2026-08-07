package smscodec

import (
	"bytes"
	"testing"
	"time"
)

func TestReassemblerAdd(t *testing.T) {
	r := NewReassembler()
	ts := time.Now()

	// Part 1 of 3
	msg, done := r.Add("+8613800138000", 42, 7, 3, 1, []byte("hello "), ts)
	if done {
		t.Fatal("premature completion after part 1")
	}
	if msg != nil {
		t.Fatalf("unexpected data after part 1: %q", msg)
	}

	// Duplicate part 1 must be ignored.
	if msg, done := r.Add("+8613800138000", 42, 7, 3, 1, []byte("x"), ts); done || msg != nil {
		t.Fatalf("duplicate part not ignored: done=%v msg=%q", done, msg)
	}

	// Part 3 out of order, then part 2 completes.
	if _, done = r.Add("+8613800138000", 42, 7, 3, 3, []byte("world!"), ts); done {
		t.Fatal("premature completion after part 3")
	}
	msg, done = r.Add("+8613800138000", 42, 7, 3, 2, []byte("beautiful "), ts)
	if !done {
		t.Fatal("expected completion after part 2")
	}
	if string(msg) != "hello beautiful world!" {
		t.Errorf("reassembled = %q", msg)
	}
	if msg, done := r.Add("+8613800138000", 42, 7, 3, 2, []byte("duplicate"), ts); done || msg != nil {
		t.Fatalf("completed message duplicate not ignored: done=%v msg=%q", done, msg)
	}
}

func TestReassemblerAdd_DifferentGroups(t *testing.T) {
	r := NewReassembler()
	ts := time.Now()

	// Different refs/seqs must not mix.
	if _, done := r.Add("A", 1, 1, 2, 1, []byte("a"), ts); done {
		t.Fatal("premature")
	}
	msg, done := r.Add("B", 9, 9, 1, 1, []byte("b"), ts)
	if !done {
		t.Fatal("single-part group should complete immediately")
	}
	if string(msg) != "b" {
		t.Errorf("single = %q", msg)
	}
}

func TestReassemblerIsolatesConcurrentMessages(t *testing.T) {
	r := NewReassembler()
	ts := time.Now()
	_, _ = r.Add("sender-a", 7, 0, 2, 1, []byte("a1"), ts)
	_, _ = r.Add("sender-a", 8, 0, 2, 1, []byte("b1"), ts)
	_, _ = r.Add("sender-b", 7, 0, 2, 1, []byte("c1"), ts)

	if got, done := r.Add("sender-a", 8, 0, 2, 2, []byte("b2"), ts); !done || string(got) != "b1b2" {
		t.Fatalf("reference-isolated message = %q, done=%v", got, done)
	}
	if got, done := r.Add("sender-b", 7, 0, 2, 2, []byte("c2"), ts); !done || string(got) != "c1c2" {
		t.Fatalf("sender-isolated message = %q, done=%v", got, done)
	}
	if got, done := r.Add("sender-a", 7, 0, 2, 2, []byte("a2"), ts); !done || string(got) != "a1a2" {
		t.Fatalf("remaining message = %q, done=%v", got, done)
	}
	_, _ = r.Add("sender-a", 9, 8, 2, 1, []byte("eight-"), ts)
	_, _ = r.Add("sender-a", 9, 16, 2, 1, []byte("sixteen-"), ts)
	if got, done := r.Add("sender-a", 9, 8, 2, 2, []byte("bit"), ts); !done || string(got) != "eight-bit" {
		t.Fatalf("8-bit reference group = %q, done=%v", got, done)
	}
	if got, done := r.Add("sender-a", 9, 16, 2, 2, []byte("bit"), ts); !done || string(got) != "sixteen-bit" {
		t.Fatalf("16-bit reference group = %q, done=%v", got, done)
	}
}

func TestReassemblerRejectsInvalidPartMetadata(t *testing.T) {
	r := NewReassembler()
	for _, partNo := range []int{-1, 0, 3} {
		if got, done := r.Add("sender", 1, 0, 2, partNo, []byte("x"), time.Now()); done || got != nil {
			t.Fatalf("part %d accepted: done=%v got=%q", partNo, done, got)
		}
	}
	if len(r.fragments) != 0 {
		t.Fatalf("invalid fragments stored: %d", len(r.fragments))
	}
}

func TestReassemblerEvictsOldestIncompleteGroupAtCapacity(t *testing.T) {
	r := NewReassembler()
	started := time.Now().Add(-time.Minute)
	for reference := 1; reference <= reassemblerMaxGroups+1; reference++ {
		r.Add("sender", uint64(reference), 8, 2, 1, []byte("part"), started.Add(time.Duration(reference)*time.Second))
	}
	if len(r.fragments) != reassemblerMaxGroups {
		t.Fatalf("fragment groups = %d", len(r.fragments))
	}
	if _, exists := r.fragments[fragmentKey("sender", 1, 8, 2)]; exists {
		t.Fatal("oldest fragment group was not evicted")
	}
}

func TestReassemblerCleanup(t *testing.T) {
	r := NewReassembler()
	old := time.Now().Add(-2 * time.Hour)
	r.Add("+8613800138000", 1, 1, 2, 1, []byte("stale"), old)

	r.Cleanup(1 * time.Hour) // drop groups older than 1h
	if len(r.fragments) != 0 {
		t.Errorf("stale group not cleaned: %d groups remain", len(r.fragments))
	}

	// A fresh partial group survives.
	r.Add("+8613800138000", 2, 2, 2, 1, []byte("fresh"), time.Now())
	r.Cleanup(1 * time.Hour)
	if len(r.fragments) != 1 {
		t.Errorf("fresh group was cleaned: %d groups remain", len(r.fragments))
	}
	_ = bytes.Compare // silence unused import
}
