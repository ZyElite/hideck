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
