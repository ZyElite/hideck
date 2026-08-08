// Package dialog implements the SIP dialog controller used by the voice
// layer to send requests and manage dialog state (RFC 3261 §12).
//
// Reconstructed from the decompiled internal/vowifi/voice/dialog.
package dialog

import (
	"context"
	"errors"
	"net"
	"sync"
)

// Controller manages one SIP dialog.
type Controller struct {
	mu       sync.RWMutex
	ctx      context.Context
	endpoint interface {
		SendRawSIP(req string) error
	}
	userAgent string
	callID    string
	fromTag   string
	toTag     string
	cseq      int
}

// NewController creates a dialog controller.
func NewController(ctx context.Context, endpoint interface {
	SendRawSIP(req string) error
}, userAgent, callID, fromTag string) *Controller {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Controller{
		ctx:       ctx,
		endpoint:  endpoint,
		userAgent: userAgent,
		callID:    callID,
		fromTag:   fromTag,
		cseq:      1,
	}
}

// SetEndpoint replaces the SIP endpoint.
func (c *Controller) SetEndpoint(ep interface {
	SendRawSIP(req string) error
}) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.endpoint = ep
	c.mu.Unlock()
}

// Context returns the dialog context.
func (c *Controller) Context() context.Context {
	if c == nil {
		return context.Background()
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ctx
}

// contextLocked returns the context without locking (internal use).
func (c *Controller) contextLocked() context.Context {
	if c == nil {
		return context.Background()
	}
	return c.ctx
}

// UserAgent returns the user agent string.
func (c *Controller) UserAgent() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.userAgent
}

// NextCSeq returns and increments the dialog CSeq.
func (c *Controller) NextCSeq() int {
	if c == nil {
		return 1
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cseq++
	return c.cseq
}

// SendDialogRequestWithHandle sends a request within the dialog.
func (c *Controller) SendDialogRequestWithHandle(req string) error {
	if c == nil {
		return errors.New("dialog: nil controller")
	}
	c.mu.RLock()
	ep := c.endpoint
	c.mu.RUnlock()
	if ep == nil {
		return errors.New("dialog: no endpoint")
	}
	return ep.SendRawSIP(req)
}

// AnswerServerInvite answers a server-side INVITE.
func (c *Controller) AnswerServerInvite() error {
	return errors.New("dialog: inbound INVITE request context is unavailable")
}

// RejectServerInvite rejects a server-side INVITE.
func (c *Controller) RejectServerInvite() error {
	return errors.New("dialog: inbound INVITE request context is unavailable")
}

// CancelClientInvite cancels a client-side INVITE.
func (c *Controller) CancelClientInvite() error {
	return errors.New("dialog: client INVITE transaction context is unavailable")
}

// CloseDialog closes the dialog.
func (c *Controller) CloseDialog() error {
	if c == nil {
		return errors.New("dialog: nil controller")
	}
	c.mu.Lock()
	c.endpoint = nil
	c.toTag = ""
	c.mu.Unlock()
	return nil
}

// SendReliableProvisionalPRACK sends a PRACK for a reliable provisional.
func (c *Controller) SendReliableProvisionalPRACK() error {
	return errors.New("dialog: reliable provisional response context is unavailable")
}

// endpointLocalIP returns the local IP of a SIP endpoint.
func endpointLocalIP(ep interface {
	SendRawSIP(req string) error
}) net.IP {
	if ep == nil {
		return nil
	}
	provider, ok := ep.(interface{ GetLocalIMSAddr() string })
	if !ok {
		return nil
	}
	host, _, err := net.SplitHostPort(provider.GetLocalIMSAddr())
	if err != nil {
		return net.ParseIP(provider.GetLocalIMSAddr())
	}
	return net.ParseIP(host)
}
