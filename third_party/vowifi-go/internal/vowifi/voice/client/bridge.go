// Package client implements the local client bridge: a SIP endpoint that
// the LAN-side client talks to, forwarding requests to the IMS network.
//
// Reconstructed from the decompiled internal/vowifi/voice/client.
package client

import (
	"errors"
	"net"
	"sync"
)

// Bridge is the client-facing SIP bridge.
type Bridge struct {
	mu       sync.RWMutex
	conn     net.PacketConn
	contact  string
	localIP  net.IP
	writeCh  chan []byte
	stop     chan struct{}
	started  bool
	endpoint interface {
		SendRawSIP(req string) error
	}
}

// NewBridge creates a client bridge.
func NewBridge() *Bridge {
	return &Bridge{
		writeCh: make(chan []byte, 64),
		stop:    make(chan struct{}),
	}
}

// SetEndpoint wires the IMS-side endpoint.
func (b *Bridge) SetEndpoint(ep interface {
	SendRawSIP(req string) error
}) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.endpoint = ep
	b.mu.Unlock()
}

// Contact returns the contact address.
func (b *Bridge) Contact() string {
	if b == nil {
		return ""
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.contact
}

// LocalIP returns the local IP.
func (b *Bridge) LocalIP() net.IP {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.localIP
}

// ListenHostPort returns the local host:port.
func (b *Bridge) ListenHostPort() string {
	if b == nil || b.conn == nil {
		return ""
	}
	return b.conn.LocalAddr().String()
}

// Start begins the bridge write worker.
func (b *Bridge) Start() error {
	if b == nil {
		return errors.New("client: nil bridge")
	}
	b.mu.Lock()
	if b.started {
		b.mu.Unlock()
		return nil
	}
	b.started = true
	b.stop = make(chan struct{})
	b.mu.Unlock()
	go b.runWriteWorker()
	return nil
}

// Stop shuts the bridge down.
func (b *Bridge) Stop() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	if !b.started {
		b.mu.Unlock()
		return nil
	}
	b.started = false
	close(b.stop)
	conn := b.conn
	b.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	return nil
}

// WriteRequest queues a request for the client.
func (b *Bridge) WriteRequest(req []byte) error {
	if b == nil {
		return errors.New("client: nil bridge")
	}
	b.mu.RLock()
	started := b.started
	b.mu.RUnlock()
	if !started {
		return errors.New("client: bridge not started")
	}
	select {
	case b.writeCh <- req:
		return nil
	default:
		return errors.New("client: write queue full")
	}
}

// StartTransaction forwards a client request to the IMS endpoint.
func (b *Bridge) StartTransaction(req []byte) error {
	if b == nil {
		return errors.New("client: nil bridge")
	}
	b.mu.RLock()
	ep := b.endpoint
	b.mu.RUnlock()
	if ep == nil {
		return errors.New("client: no endpoint")
	}
	return ep.SendRawSIP(string(req))
}

// SendPush sends a push notification to the client.
func (b *Bridge) SendPush(payload []byte) error {
	return b.WriteRequest(payload)
}

// runWriteWorker drains the write queue.
func (b *Bridge) runWriteWorker() {
	for {
		select {
		case <-b.stop:
			return
		case req := <-b.writeCh:
			b.mu.RLock()
			conn := b.conn
			b.mu.RUnlock()
			if conn != nil {
				_, _ = conn.WriteTo(req, conn.LocalAddr())
			}
		}
	}
}
