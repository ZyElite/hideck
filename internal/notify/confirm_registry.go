package notify

import (
	"sync"
	"time"
)

// pendingConfirm holds an in-flight confirmation for a two-step command.
type pendingConfirm struct {
	created time.Time
	confirm chan bool
}

// confirmRegistry tracks pending confirmations by user identifier so that
// a follow-up "/y" command can resolve the blocked Confirm call.
type confirmRegistry struct {
	mu      sync.Mutex
	pending map[string]*pendingConfirm
}

func newConfirmRegistry() *confirmRegistry {
	return &confirmRegistry{pending: make(map[string]*pendingConfirm)}
}

// register creates a pending confirmation for the given user key and blocks
// until either the user confirms (via Resolve), the timeout expires, or the
// caller gives up.
func (r *confirmRegistry) register(userKey, prompt string, reply func(string), timeout time.Duration) bool {
	r.mu.Lock()
	if _, exists := r.pending[userKey]; exists {
		r.mu.Unlock()
		reply("已有待确认操作，请先回复 y 或 n")
		return false
	}
	ch := make(chan bool, 1)
	r.pending[userKey] = &pendingConfirm{created: time.Now(), confirm: ch}
	r.mu.Unlock()

	reply(prompt)

	select {
	case ok := <-ch:
		return ok
	case <-time.After(timeout):
		r.mu.Lock()
		delete(r.pending, userKey)
		r.mu.Unlock()
		reply("确认超时，操作已取消")
		return false
	}
}

// resolve attempts to resolve a pending confirmation for the given user key.
// Returns true if a pending confirmation was found and resolved.
func (r *confirmRegistry) resolve(userKey string, confirmed bool) bool {
	r.mu.Lock()
	pc, ok := r.pending[userKey]
	if !ok {
		r.mu.Unlock()
		return false
	}
	delete(r.pending, userKey)
	r.mu.Unlock()

	select {
	case pc.confirm <- confirmed:
	default:
	}
	return true
}
