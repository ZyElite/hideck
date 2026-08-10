package client

import (
	"errors"
	"net"
	"sync"
)

const packetWriteQueueSize = 64

// TransportConfig configures the additive raw-packet bridge retained for
// callers of the interim implementation.
type TransportConfig struct {
	Conn    net.PacketConn
	Remote  net.Addr
	Contact string
	LocalIP net.IP
}

// RawSIPEndpoint accepts an unparsed SIP request from PacketBridge.
type RawSIPEndpoint interface {
	SendRawSIP(req string) error
}

// PacketBridge retains the interim packet API without changing the recovered
// v1.5.5 Bridge layout or method set.
type PacketBridge struct {
	mu       sync.RWMutex
	conn     net.PacketConn
	remote   net.Addr
	contact  string
	localIP  net.IP
	writeCh  chan []byte
	stop     chan struct{}
	wg       sync.WaitGroup
	started  bool
	writeErr error
	endpoint RawSIPEndpoint
}

// NewPacketBridge creates the additive raw-packet bridge.
func NewPacketBridge() *PacketBridge {
	return &PacketBridge{}
}

func (b *PacketBridge) ConfigureTransport(config TransportConfig) error {
	if b == nil {
		return errors.New("client: nil packet bridge")
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

func (b *PacketBridge) SetEndpoint(endpoint RawSIPEndpoint) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.endpoint = endpoint
	b.mu.Unlock()
}

func (b *PacketBridge) Contact() string {
	if b == nil {
		return ""
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.contact
}

func (b *PacketBridge) LocalIP() net.IP {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return append(net.IP(nil), b.localIP...)
}

func (b *PacketBridge) ListenHostPort() string {
	if b == nil {
		return ""
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.conn == nil {
		return ""
	}
	return b.conn.LocalAddr().String()
}

func (b *PacketBridge) Start() error {
	if b == nil {
		return errors.New("client: nil packet bridge")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.started {
		return nil
	}
	if b.conn == nil || b.remote == nil {
		return errors.New("client: packet transport is not configured")
	}
	b.started = true
	b.stop = make(chan struct{})
	b.writeCh = make(chan []byte, packetWriteQueueSize)
	b.wg.Add(1)
	go b.runPacketWriter(b.writeCh, b.stop)
	return nil
}

func (b *PacketBridge) Stop() error {
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
	b.wg.Wait()
	b.mu.Lock()
	b.writeCh = nil
	b.stop = nil
	b.mu.Unlock()
	return nil
}

func (b *PacketBridge) WriteRequest(req []byte) error {
	if b == nil {
		return errors.New("client: nil packet bridge")
	}
	b.mu.RLock()
	started, writeCh := b.started, b.writeCh
	b.mu.RUnlock()
	if !started {
		return errors.New("client: packet bridge not started")
	}
	select {
	case writeCh <- append([]byte(nil), req...):
		return nil
	default:
		return errors.New("client: packet write queue full")
	}
}

func (b *PacketBridge) StartTransaction(req []byte) error {
	if b == nil {
		return errors.New("client: nil packet bridge")
	}
	b.mu.RLock()
	endpoint := b.endpoint
	b.mu.RUnlock()
	if endpoint == nil {
		return errors.New("client: no raw SIP endpoint")
	}
	return endpoint.SendRawSIP(string(req))
}

func (b *PacketBridge) SendPush(payload []byte) error {
	return b.WriteRequest(payload)
}

func (b *PacketBridge) LastWriteError() error {
	if b == nil {
		return errors.New("client: nil packet bridge")
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.writeErr
}

func (b *PacketBridge) runPacketWriter(writeCh <-chan []byte, stop <-chan struct{}) {
	defer b.wg.Done()
	for {
		select {
		case <-stop:
			return
		case request := <-writeCh:
			b.writePacket(request)
		}
	}
}

func (b *PacketBridge) writePacket(request []byte) {
	b.mu.RLock()
	conn, remote := b.conn, b.remote
	b.mu.RUnlock()
	if conn == nil || remote == nil {
		return
	}
	if _, err := conn.WriteTo(request, remote); err != nil {
		b.mu.Lock()
		b.writeErr = err
		b.mu.Unlock()
	}
}
