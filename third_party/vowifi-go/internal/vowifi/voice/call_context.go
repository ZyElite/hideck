package voice

import (
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/media"
)

// SetIMSDialog attaches the recovered IMS dialog handle interface.
func (c *Call) SetIMSDialog(h imsendpoint.DialogHandle) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.DialogState.IMSDialog = h
	c.imsDialog, _ = h.(*imscore.DialogHandle)
	c.mu.Unlock()
}

// IMSDialog returns the additive concrete IMS dialog handle.
func (c *Call) IMSDialog() *imscore.DialogHandle {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.imsDialog
}

// SetIMSInviteHandle attaches the recovered IMS invite handle interface.
func (c *Call) SetIMSInviteHandle(h imsendpoint.InviteHandle) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.DialogState.IMSInviteHandle = h
	c.imsInvite, _ = h.(*imscore.InviteHandle)
	c.mu.Unlock()
}

// IMSInviteHandle returns the additive concrete IMS invite handle.
func (c *Call) IMSInviteHandle() *imscore.InviteHandle {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.imsInvite
}

// SetRouteSet sets the dialog route set.
func (c *Call) SetRouteSet(route []string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.routeSet = append([]string{}, route...)
	c.DialogState.RouteSet = append([]string{}, route...)
	c.mu.Unlock()
}

// RouteSet returns the dialog route set.
func (c *Call) RouteSet() []string {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]string{}, c.routeSet...)
}

// SetRTPRelay attaches the media relay.
func (c *Call) SetRTPRelay(relay *media.RTPRelay) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.rtpRelay = relay
	c.MediaState.RTPRelay = relay
	c.mu.Unlock()
}

// RTPRelay returns the media relay.
func (c *Call) RTPRelay() *media.RTPRelay {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.rtpRelay
}

func (c *Call) setComfortNoise(generator *media.ComfortNoiseGenerator) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.comfortNoise = generator
	c.mu.Unlock()
}

// MediaErrors reports asynchronous media generation failures.
func (c *Call) MediaErrors() <-chan error {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	generator := c.comfortNoise
	c.mu.RUnlock()
	if generator == nil {
		return nil
	}
	return generator.Errors()
}
