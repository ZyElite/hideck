// Package swu implements the SWu (UE <-> ePDG) IKEv2 + EAP-AKA client session
// that establishes the VoWiFi tunnel.
//
// Reconstructed from the decompiled engine/swu. The foundational, self-contained
// pieces (error types, fragment reassembly, algorithm-policy helpers, NAI
// construction) are fully implemented; the Session state machine is filled in
// incrementally (see re/REWRITE.md).
package swu

import (
	"context"
	"sync"
	"time"
)

// TaskManager serialises IKE requests and drives retransmission/windowing
// (RFC 7296 §2.2). Fields used by task_manager.go.
type TaskManager struct {
	ctx        context.Context
	cancel     context.CancelFunc
	send       func(messageID uint32, message []byte) error
	config     RetransmitConfig
	windowSize int

	mu     sync.Mutex
	active map[uint32]*task // by IKE message id
	queue  []*task

	trigger chan struct{} // wakes the retransmit loop
	stop    chan struct{}
	wg      sync.WaitGroup

	tickInterval time.Duration // retransmit poll interval (500ms in production)
}
