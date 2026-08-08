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
	remote   net.Addr
	contact  string
	localIP  net.IP
	writeCh  chan []byte
	stop     chan struct{}
	started  bool
	writeErr error
	endpoint interface {
		SendRawSIP(req string) error
	}
}

// TransportConfig injects the LAN-side packet transport owned by the bridge.
type TransportConfig struct {
	Conn    net.PacketConn
	Remote  net.Addr
	Contact string
	LocalIP net.IP
}

// NewBridge creates a client bridge.
func NewBridge() *Bridge {
	return &Bridge{}
}

// ConfigureTransport configures the real client-facing packet path.
func (b *Bridge) ConfigureTransport(config TransportConfig) error {
	if b == nil {
		return errors.New("client: nil bridge")
	}
	if config.Conn == nil || config.Remote == nil {
		return errors.New("client: packet connection and remote address are required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.started {
		return errors.New("client: cannot replace transport while started")
	}
	b.conn = config.Conn
	b.remote = config.Remote
	b.contact = config.Contact
	b.localIP = append(net.IP(nil), config.LocalIP...)
	b.writeErr = nil
	return nil
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
	return append(net.IP(nil), b.localIP...)
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
	if b.conn == nil || b.remote == nil {
		b.mu.Unlock()
		return errors.New("client: packet transport is not configured")
	}
	b.started = true
	b.stop = make(chan struct{})
	b.writeCh = make(chan []byte, 64)
	writeCh, stop := b.writeCh, b.stop
	b.mu.Unlock()
	go b.runWriteWorker(writeCh, stop)
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
	b.conn = nil
	b.remote = nil
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
	writeCh := b.writeCh
	b.mu.RUnlock()
	if !started {
		return errors.New("client: bridge not started")
	}
	select {
	case writeCh <- append([]byte(nil), req...):
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

// LastWriteError returns the most recent asynchronous packet write failure.
func (b *Bridge) LastWriteError() error {
	if b == nil {
		return errors.New("client: nil bridge")
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.writeErr
}

// runWriteWorker drains the write queue.
func (b *Bridge) runWriteWorker(writeCh <-chan []byte, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case req := <-writeCh:
			b.mu.RLock()
			conn, remote := b.conn, b.remote
			b.mu.RUnlock()
			if conn != nil && remote != nil {
				if _, err := conn.WriteTo(req, remote); err != nil {
					b.mu.Lock()
					b.writeErr = err
					b.mu.Unlock()
				}
			}
		}
	}
}
