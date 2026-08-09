package swu

import (
	"context"
	"errors"
	"sync"

	"github.com/iniwex5/vowifi-go/engine/ipsec"
	"github.com/iniwex5/vowifi-go/engine/logger"
	"go.uber.org/zap"
)

const userspaceInboundQueueSize = 128

var (
	errInnerPacketEndpointClosed = errors.New("inner packet endpoint closed")
	errInnerPacketSAMissing      = errors.New("inner packet security association missing")
)

// InnerPacketEndpoint is the original packet boundary exposed by Session.
type InnerPacketEndpoint interface {
	Close() error
	ReadPacket(context.Context) ([]byte, error)
	Snapshot() InnerPacketSnapshot
	WritePacket(context.Context, []byte) error
}

// InnerPacketIO retains the additive packet boundary used by the current host.
type InnerPacketIO interface {
	ReadPacketContext(context.Context) ([]byte, error)
	WritePacketContext(context.Context, []byte) error
}

type userspaceInnerPacketEndpoint struct {
	session          *Session
	transport        ipsec.Transport
	inbound          chan []byte
	done             chan struct{}
	transportChanged chan struct{}
	closeMu          sync.RWMutex
	closed           bool
	stats            dataPlaneRuntimeStats
}

func newUserspaceInnerPacketEndpoint(
	session *Session,
	transport ipsec.Transport,
	inboundBuffer int,
) *userspaceInnerPacketEndpoint {
	if inboundBuffer <= 0 {
		inboundBuffer = userspaceInboundQueueSize
	}
	return &userspaceInnerPacketEndpoint{
		session: session, transport: transport,
		inbound: make(chan []byte, inboundBuffer), done: make(chan struct{}),
		transportChanged: make(chan struct{}, 1),
	}
}

func (e *userspaceInnerPacketEndpoint) start() {
	e.session.dataPlaneWG.Add(3)
	go func() {
		defer e.session.dataPlaneWG.Done()
		e.readOuterESP()
	}()
	go func() {
		defer e.session.dataPlaneWG.Done()
		e.session.logDataPlaneStats(
			e.session.ctx, DataplaneModeUserspace, &e.stats, dataPlaneStatsInterval,
		)
	}()
	go func() {
		defer e.session.dataPlaneWG.Done()
		select {
		case <-e.session.ctx.Done():
			_ = e.Close()
		case <-e.done:
		}
	}()
}

func (e *userspaceInnerPacketEndpoint) WritePacket(ctx context.Context, packet []byte) error {
	ctx = packetContext(ctx)
	e.closeMu.RLock()
	defer e.closeMu.RUnlock()
	if e.closed {
		return errInnerPacketEndpointClosed
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	e.stats.tunRead.Add(1)
	e.stats.lastTunReadLen.Store(uint64(len(packet)))
	lease, err := e.session.encapsulateInnerPacketLease(packet)
	if err != nil {
		e.recordEncapsulationError(err)
		return err
	}
	transport := e.transport
	if transport == nil {
		lease.Release()
		e.stats.espSendError.Add(1)
		return errors.New("userspace data plane socket is nil")
	}
	err = transport.SendESP(lease.data)
	lease.Release()
	if err != nil {
		e.stats.espSendError.Add(1)
		return err
	}
	e.session.markOutboundActivity()
	e.stats.espSend.Add(1)
	return nil
}

func (e *userspaceInnerPacketEndpoint) ReadPacket(ctx context.Context) ([]byte, error) {
	ctx = packetContext(ctx)
	if e.isClosed() {
		return nil, errInnerPacketEndpointClosed
	}
	select {
	case packet := <-e.inbound:
		if e.isClosed() {
			return nil, errInnerPacketEndpointClosed
		}
		return append([]byte(nil), packet...), nil
	case <-e.done:
		return nil, errInnerPacketEndpointClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (e *userspaceInnerPacketEndpoint) ReadPacketContext(ctx context.Context) ([]byte, error) {
	return e.ReadPacket(ctx)
}

func (e *userspaceInnerPacketEndpoint) WritePacketContext(ctx context.Context, packet []byte) error {
	return e.WritePacket(ctx, packet)
}

func (e *userspaceInnerPacketEndpoint) Close() error {
	e.closeMu.Lock()
	defer e.closeMu.Unlock()
	if !e.closed {
		e.closed = true
		close(e.done)
	}
	return nil
}

func (e *userspaceInnerPacketEndpoint) isClosed() bool {
	e.closeMu.RLock()
	defer e.closeMu.RUnlock()
	return e.closed
}

func (e *userspaceInnerPacketEndpoint) Snapshot() InnerPacketSnapshot {
	return e.stats.innerPacketSnapshot()
}

func (e *userspaceInnerPacketEndpoint) readOuterESP() {
	transport := e.currentTransport()
	for transport != nil {
		select {
		case packet, ok := <-transport.ESPPackets():
			if !ok {
				transport = e.session.replacementTransport(transport)
				if transport == nil {
					return
				}
				e.rebindTransport(transport)
				continue
			}
			e.handleOuterESP(packet)
		case <-e.transportChanged:
			transport = e.currentTransport()
		case <-e.done:
			return
		}
	}
}

func (e *userspaceInnerPacketEndpoint) handleOuterESP(packet []byte) {
	e.session.markInboundActivity()
	e.stats.espIn.Add(1)
	inner, spi, err := e.session.decapsulateOuterESP(packet)
	e.stats.lastInSPI.Store(spi)
	if err != nil {
		e.recordDecapsulationError(err)
		logger.Warn("userspace ESP decapsulation failed",
			zap.Uint32("spi", spi), zap.Int("packet_len", len(packet)), zap.Error(err))
		return
	}
	e.stats.lastPlainLen.Store(uint64(len(inner)))
	owned := append([]byte(nil), inner...)
	select {
	case e.inbound <- owned:
		e.stats.tunWrite.Add(1)
	default:
		e.stats.tunWriteError.Add(1)
		e.stats.espSendError.Add(1)
		logger.Warn("userspace inner packet receive queue full; dropping packet",
			zap.Uint32("spi", spi), zap.Int("packet_len", len(inner)))
	}
}

func (e *userspaceInnerPacketEndpoint) currentTransport() ipsec.Transport {
	e.closeMu.RLock()
	defer e.closeMu.RUnlock()
	return e.transport
}

func (e *userspaceInnerPacketEndpoint) rebindTransport(transport ipsec.Transport) {
	e.closeMu.Lock()
	if e.closed {
		e.closeMu.Unlock()
		return
	}
	e.transport = transport
	e.closeMu.Unlock()
	select {
	case e.transportChanged <- struct{}{}:
	default:
	}
}

func (e *userspaceInnerPacketEndpoint) recordEncapsulationError(err error) {
	e.stats.recordEncapsulationError(err)
}

func (e *userspaceInnerPacketEndpoint) recordDecapsulationError(err error) {
	e.stats.recordDecapsulationError(err)
}

func packetContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (s *Session) startUserspaceDataPlane() error {
	transport := s.transport()
	if transport == nil {
		return errors.New("userspace data plane socket is nil")
	}
	endpoint := newUserspaceInnerPacketEndpoint(s, transport, userspaceInboundQueueSize)
	previous, started, err := s.installUserspaceEndpoint(endpoint)
	if err != nil || !started {
		return err
	}
	if previous != nil {
		_ = previous.Close()
	}
	logger.Info("userspace inner packet endpoint started")
	return nil
}

func (s *Session) installUserspaceEndpoint(
	endpoint *userspaceInnerPacketEndpoint,
) (*userspaceInnerPacketEndpoint, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == stateShutdown {
		return nil, false, context.Canceled
	}
	if err := s.ctx.Err(); err != nil {
		return nil, false, err
	}
	if s.dataPlaneStarted {
		return nil, false, nil
	}
	s.innerEndpointMu.Lock()
	previous := s.innerEndpoint
	s.innerEndpoint = endpoint
	s.innerEndpointMu.Unlock()
	s.dataPlaneStarted = true
	endpoint.start()
	return previous, true, nil
}

func (s *Session) swapInnerPacketEndpoint(endpoint *userspaceInnerPacketEndpoint) *userspaceInnerPacketEndpoint {
	s.innerEndpointMu.Lock()
	previous := s.innerEndpoint
	s.innerEndpoint = endpoint
	s.innerEndpointMu.Unlock()
	return previous
}

func (s *Session) currentInnerPacketEndpoint() *userspaceInnerPacketEndpoint {
	s.innerEndpointMu.RLock()
	defer s.innerEndpointMu.RUnlock()
	return s.innerEndpoint
}

func (s *Session) rebindUserspaceTransport(transport ipsec.Transport) {
	if endpoint := s.currentInnerPacketEndpoint(); endpoint != nil {
		endpoint.rebindTransport(transport)
	}
}

// InnerPacketEndpoint returns the original userspace packet boundary.
func (s *Session) InnerPacketEndpoint() InnerPacketEndpoint {
	endpoint := s.currentInnerPacketEndpoint()
	if endpoint == nil {
		return nil
	}
	return endpoint
}
