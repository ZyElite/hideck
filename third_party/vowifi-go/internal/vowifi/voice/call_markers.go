package voice

import (
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

// MarkACKSent records that the ACK was sent.
func (c *Call) MarkACKSent() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.ackSent = true
	c.DialogState.ACKSent = true
	c.mu.Unlock()
}

// IsACKSent reports whether the ACK was sent.
func (c *Call) IsACKSent() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ackSent
}

// MarkInviteFinalSeen records the recovered one-shot error final response.
func (c *Call) MarkInviteFinalSeen() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inviteFinalSeen {
		return false
	}
	c.inviteFinalSeen = true
	c.DialogState.InviteFinalSeen = true
	c.transitionLocked(int(callstate.StateEnded))
	return true
}

// HasInviteFinalSeen reports whether the INVITE error final was seen.
func (c *Call) HasInviteFinalSeen() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.inviteFinalSeen
}

// MarkInviteProvisional records and applies the recovered provisional state.
func (c *Call) MarkInviteProvisional(status int) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inviteProvisional = true
	c.DialogState.InviteProvisional = true
	switch status {
	case 180:
		c.transitionLocked(int(callstate.StateAlerting))
	case 183:
		c.transitionLocked(int(callstate.StateConnecting))
	default:
		if c.State == int(callstate.StateIdle) {
			c.transitionLocked(int(callstate.StateDialing))
		}
	}
}

// HasInviteProvisional reports whether a provisional was seen.
func (c *Call) HasInviteProvisional() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.inviteProvisional
}

// MarkLocalCancelSent records the recovered one-shot local CANCEL outcome.
func (c *Call) MarkLocalCancelSent(reason string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.localCancelSent {
		return false
	}
	c.localCancelSent = true
	c.DialogState.LocalCancelSent = true
	reason = strings.TrimSpace(reason)
	c.outboundCancelReason = reason
	c.DialogState.LocalCancelReason = reason
	c.transitionLocked(int(callstate.StateFailed))
	return true
}

// HasLocalCancelSent reports whether a local CANCEL was sent.
func (c *Call) HasLocalCancelSent() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.localCancelSent
}

// MarkReliableProvisional retains the recovered latest-RSeq deduplication.
func (c *Call) MarkReliableProvisional(rseq uint32) bool {
	if c == nil || rseq == 0 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Timers.RSeq == rseq {
		return false
	}
	c.Timers.RSeq = rseq
	c.reliableProvisional = true
	return true
}

// HasReliableProvisional reports whether this call negotiated 100rel.
func (c *Call) HasReliableProvisional() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.reliableProvisional
}

func (c *Call) markReliableProvisionalRSeq(rseq uint32) bool {
	return c.MarkReliableProvisional(rseq)
}

// SetOutboundCancelReason records the additive outbound cancel reason.
func (c *Call) SetOutboundCancelReason(reason string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.outboundCancelReason = reason
	c.DialogState.LocalCancelReason = reason
	c.mu.Unlock()
}

// OutboundCancelReason returns the outbound cancel reason.
func (c *Call) OutboundCancelReason() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.outboundCancelReason
}
