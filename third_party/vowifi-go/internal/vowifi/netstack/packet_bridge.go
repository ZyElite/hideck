package netstack

import (
	"errors"
	"sync"
	"sync/atomic"
)

// PacketBridge carries inner IP packets between a stack and a tunnel.
type PacketBridge struct {
	mu          sync.RWMutex
	transformer PacketTransformer
	inbound     chan []byte
	outbound    chan []byte
	closed      chan struct{}
	closeOnce   sync.Once
	stats       bridgeStats
}

type BridgeStats struct {
	InboundPackets  uint64
	OutboundPackets uint64
}

type bridgeStats struct {
	InboundPackets  atomic.Uint64
	OutboundPackets atomic.Uint64
}

type PacketTransformer interface {
	TransformOutbound(inner []byte) ([]byte, error)
	TransformInbound(tunnel []byte) ([]byte, error)
}

func NewPacketBridge() *PacketBridge {
	return &PacketBridge{
		inbound: make(chan []byte, 128), outbound: make(chan []byte, 128), closed: make(chan struct{}),
	}
}

func (b *PacketBridge) SetTransformer(transformer PacketTransformer) {
	b.mu.Lock()
	b.transformer = transformer
	b.mu.Unlock()
}

func (b *PacketBridge) currentTransformer() PacketTransformer {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.transformer
}

func (b *PacketBridge) Inbound() <-chan []byte  { return b.inbound }
func (b *PacketBridge) Outbound() <-chan []byte { return b.outbound }

func (b *PacketBridge) InjectInboundPacket(packet []byte) error {
	select {
	case <-b.closed:
		return errors.New("netstack: bridge closed")
	case b.inbound <- packet:
		b.stats.InboundPackets.Add(1)
		return nil
	}
}

func (b *PacketBridge) WriteOutboundPacket(packet []byte) error {
	select {
	case <-b.closed:
		return errors.New("netstack: bridge closed")
	case b.outbound <- packet:
		b.stats.OutboundPackets.Add(1)
		return nil
	}
}

func (b *PacketBridge) outboundLoop(forward func([]byte) error) {
	for {
		select {
		case <-b.closed:
			return
		case packet := <-b.outbound:
			packet, ok := b.transformOutbound(packet)
			if ok && forward != nil {
				_ = forward(packet)
			}
		}
	}
}

func (b *PacketBridge) inboundLoop(deliver func([]byte)) {
	for {
		select {
		case <-b.closed:
			return
		case packet := <-b.inbound:
			packet, ok := b.transformInbound(packet)
			if ok && deliver != nil {
				deliver(packet)
			}
		}
	}
}

func (b *PacketBridge) transformOutbound(packet []byte) ([]byte, bool) {
	transformer := b.currentTransformer()
	if transformer == nil {
		return packet, true
	}
	transformed, err := transformer.TransformOutbound(packet)
	return transformed, err == nil
}

func (b *PacketBridge) transformInbound(packet []byte) ([]byte, bool) {
	transformer := b.currentTransformer()
	if transformer == nil {
		return packet, true
	}
	transformed, err := transformer.TransformInbound(packet)
	return transformed, err == nil
}

func (b *PacketBridge) Start(forward func([]byte) error, deliver func([]byte)) {
	go b.outboundLoop(forward)
	go b.inboundLoop(deliver)
}

func (b *PacketBridge) Stats() BridgeStats {
	return BridgeStats{
		InboundPackets: b.stats.InboundPackets.Load(), OutboundPackets: b.stats.OutboundPackets.Load(),
	}
}

func (b *PacketBridge) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}
