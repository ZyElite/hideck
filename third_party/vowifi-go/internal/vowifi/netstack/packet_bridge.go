package netstack

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/iniwex5/vowifi-go/engine/swu"
)

type PacketTransformer interface {
	TransformInbound([]byte) ([]byte, bool, error)
	TransformOutbound([]byte) ([]byte, bool, error)
}

type PacketBridgeStats struct {
	OutboundPackets uint64
	InboundPackets  uint64
	OutboundErrors  uint64
	InboundErrors   uint64
}

type Stats struct {
	OutboundPackets uint64
	InboundPackets  uint64
	Bridge          PacketBridgeStats
	PacketsIn       uint64
	PacketsOut      uint64
	BytesIn         uint64
	BytesOut        uint64
}

// Compatibility aliases retain the names introduced by the current tree.
type BridgeStats = PacketBridgeStats
type NetworkStats = Stats

type PacketBridge struct {
	ctx       context.Context
	cancel    context.CancelFunc
	link      *channel.Endpoint
	endpoint  swu.InnerPacketEndpoint
	mu        sync.RWMutex
	transform PacketTransformer
	parent    *Network
	wg        sync.WaitGroup

	outboundPackets atomic.Uint64
	inboundPackets  atomic.Uint64
	outboundErrors  atomic.Uint64
	inboundErrors   atomic.Uint64
}

func NewPacketBridge(
	ctx context.Context,
	link *channel.Endpoint,
	endpoint swu.InnerPacketEndpoint,
	transformer PacketTransformer,
	parent *Network,
) *PacketBridge {
	if ctx == nil {
		ctx = context.Background()
	}
	bridgeCtx, cancel := context.WithCancel(ctx)
	b := &PacketBridge{
		ctx: bridgeCtx, cancel: cancel, link: link, endpoint: endpoint,
		transform: transformer, parent: parent,
	}
	b.wg.Add(2)
	go b.outboundLoop()
	go b.inboundLoop()
	return b
}

func (b *PacketBridge) Close() {
	if b == nil {
		return
	}
	b.cancel()
	if b.endpoint != nil {
		_ = b.endpoint.Close()
	}
	b.wg.Wait()
}

func (b *PacketBridge) Stats() PacketBridgeStats {
	if b == nil {
		return PacketBridgeStats{}
	}
	return PacketBridgeStats{
		OutboundPackets: b.outboundPackets.Load(),
		InboundPackets:  b.inboundPackets.Load(),
		OutboundErrors:  b.outboundErrors.Load(),
		InboundErrors:   b.inboundErrors.Load(),
	}
}

func (b *PacketBridge) SetTransformer(transformer PacketTransformer) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.transform = transformer
	b.mu.Unlock()
}

func (b *PacketBridge) currentTransformer() PacketTransformer {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.transform
}

func (b *PacketBridge) outboundLoop() {
	defer b.wg.Done()
	for {
		packet := b.link.ReadContext(b.ctx)
		if packet == nil {
			return
		}
		if err := b.writeOutboundPacket(packet); err != nil && !errors.Is(err, context.Canceled) {
			b.outboundErrors.Add(1)
		}
	}
}

func (b *PacketBridge) writeOutboundPacket(packet *stack.PacketBuffer) error {
	defer packet.DecRef()
	view := packet.ToView()
	defer view.Release()
	data := append([]byte(nil), view.AsSlice()...)
	if transformer := b.currentTransformer(); transformer != nil {
		var err error
		data, _, err = transformer.TransformOutbound(data)
		if err != nil {
			return err
		}
	}
	if err := b.endpoint.WritePacket(b.ctx, data); err != nil {
		return err
	}
	b.outboundPackets.Add(1)
	if b.parent != nil {
		b.parent.outboundPackets.Add(1)
		b.parent.outboundBytes.Add(uint64(len(data)))
	}
	return nil
}

func (b *PacketBridge) inboundLoop() {
	defer b.wg.Done()
	for {
		packet, err := b.endpoint.ReadPacket(b.ctx)
		if err != nil {
			if b.ctx.Err() != nil {
				return
			}
			b.inboundErrors.Add(1)
			continue
		}
		if err := b.injectInboundPacket(packet); err != nil {
			b.inboundErrors.Add(1)
		}
	}
}

func (b *PacketBridge) injectInboundPacket(data []byte) error {
	if len(data) == 0 {
		return errors.New("netstack: empty inbound packet")
	}
	if transformer := b.currentTransformer(); transformer != nil {
		var err error
		data, _, err = transformer.TransformInbound(data)
		if err != nil {
			return err
		}
	}
	if len(data) == 0 {
		return errors.New("netstack: empty packet")
	}
	protocol, err := inboundProtocol(data[0] >> 4)
	if err != nil {
		return err
	}
	packet := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(data)})
	b.link.InjectInbound(protocol, packet)
	packet.DecRef()
	b.inboundPackets.Add(1)
	if b.parent != nil {
		b.parent.inboundPackets.Add(1)
		b.parent.inboundBytes.Add(uint64(len(data)))
	}
	return nil
}

func inboundProtocol(version byte) (tcpip.NetworkProtocolNumber, error) {
	switch version {
	case 4:
		return ipv4.ProtocolNumber, nil
	case 6:
		return ipv6.ProtocolNumber, nil
	default:
		return 0, errors.New("netstack: unsupported IP version")
	}
}
