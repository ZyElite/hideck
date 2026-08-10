// Package dialog adapts the voice layer to the retained IMS dialog endpoint.
package dialog

import (
	"strings"
	"sync"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsdialog"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

// Controller owns the registration-derived headers shared by voice dialogs.
type Controller struct {
	deviceID string
	endpoint imsendpoint.ClientDialogEndpoint

	mu               sync.Mutex
	cachedFromURI    sip.Uri
	cachedContactHdr *sip.ContactHeader
	cachedRouteHdr   sip.Header
	lastSessionHash  string
}

// NewController creates a controller bound to an IMS endpoint.
func NewController(deviceID string, endpoint imsendpoint.ClientDialogEndpoint) *Controller {
	controller := &Controller{deviceID: strings.TrimSpace(deviceID)}
	controller.SetEndpoint(endpoint)
	return controller
}

// SetEndpoint replaces the IMS endpoint and invalidates registration caches.
func (c *Controller) SetEndpoint(endpoint imsendpoint.ClientDialogEndpoint) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.endpoint = endpoint
	c.cachedFromURI = sip.Uri{}
	c.cachedContactHdr = nil
	c.cachedRouteHdr = nil
	c.lastSessionHash = ""
	c.mu.Unlock()
}

// Context returns the current immutable dialog-building context.
func (c *Controller) Context() imsdialog.Context {
	if c == nil {
		return imsdialog.Context{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.contextLocked()
}

// UserAgent returns the endpoint's current User-Agent value.
func (c *Controller) UserAgent() string {
	endpoint := c.currentEndpoint()
	if endpoint == nil {
		return ""
	}
	return strings.TrimSpace(endpoint.Snapshot().UserAgent)
}

// NextCSeq reserves the next endpoint-wide SIP sequence number.
func (c *Controller) NextCSeq() uint32 {
	endpoint := c.currentEndpoint()
	if endpoint == nil {
		return 0
	}
	return endpoint.NextCSeq()
}

func (c *Controller) currentEndpoint() imsendpoint.ClientDialogEndpoint {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	endpoint := c.endpoint
	c.mu.Unlock()
	return endpoint
}
