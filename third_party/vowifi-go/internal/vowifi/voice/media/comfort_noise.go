package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

const (
	comfortNoisePacketInterval = 20 * time.Millisecond
	comfortNoiseSamples        = 160
	comfortNoiseSSRC           = 0xdeadbeef
)

// NewComfortNoiseGenerator accepts the original connection/address/context
// form and an empty additive form configured by Start.
func NewComfortNoiseGenerator(args ...any) *ComfortNoiseGenerator {
	now := uint32(time.Now().UnixNano())
	generator := &ComfortNoiseGenerator{
		timestamp: now, ssrc: comfortNoiseSSRC, seed: now,
		stopCh: make(chan struct{}), errors: make(chan error, 1),
	}
	if len(args) == 4 {
		generator.conn, _ = args[0].(net.PacketConn)
		generator.remoteAddr, _ = args[1].(*net.UDPAddr)
		generator.deviceID, _ = args[2].(string)
		generator.traceID, _ = args[3].(string)
	}
	return generator
}

// Start begins generation, optionally configuring the additive conn/address.
func (g *ComfortNoiseGenerator) Start(args ...any) error {
	if g == nil {
		return errors.New("media: nil noise generator")
	}
	g.mu.Lock()
	select {
	case <-g.stopCh:
		g.mu.Unlock()
		return errors.New("media: comfort-noise generator stopped")
	default:
	}
	if len(args) == 2 {
		g.conn, _ = args[0].(net.PacketConn)
		g.remoteAddr, _ = args[1].(*net.UDPAddr)
	}
	if g.conn == nil || g.remoteAddr == nil {
		g.mu.Unlock()
		return errors.New("media: comfort-noise connection and destination are required")
	}
	if g.started {
		g.mu.Unlock()
		return nil
	}
	g.started = true
	g.wg.Add(1)
	g.mu.Unlock()
	go g.sendLoop()
	return nil
}

func (g *ComfortNoiseGenerator) Stop() {
	if g == nil {
		return
	}
	g.stopOnce.Do(func() { close(g.stopCh) })
	g.wg.Wait()
	g.mu.Lock()
	g.started = false
	g.mu.Unlock()
}

func (g *ComfortNoiseGenerator) Errors() <-chan error {
	if g == nil {
		return nil
	}
	return g.errors
}

func (g *ComfortNoiseGenerator) sendLoop() {
	defer func() {
		g.mu.Lock()
		g.started = false
		g.mu.Unlock()
		g.wg.Done()
	}()
	ticker := time.NewTicker(comfortNoisePacketInterval)
	defer ticker.Stop()
	for {
		select {
		case <-g.stopCh:
			return
		case <-ticker.C:
			if err := g.sendOnePacket(); err != nil {
				g.stopOnce.Do(func() { close(g.stopCh) })
				g.reportError(err)
				return
			}
		}
	}
}

func (g *ComfortNoiseGenerator) sendOnePacket() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	packet := make([]byte, 12+comfortNoiseSamples)
	packet[0] = 0x80
	binary.BigEndian.PutUint16(packet[2:4], g.seqNum)
	binary.BigEndian.PutUint32(packet[4:8], g.timestamp)
	binary.BigEndian.PutUint32(packet[8:12], g.ssrc)
	copy(packet[12:], g.generateComfortNoiseUlaw(comfortNoiseSamples))
	if _, err := g.conn.WriteTo(packet, g.remoteAddr); err != nil {
		return fmt.Errorf("media: write PCMU RTP: %w", err)
	}
	g.seqNum++
	g.timestamp += comfortNoiseSamples
	return nil
}

func (g *ComfortNoiseGenerator) reportError(err error) {
	select {
	case g.errors <- err:
	default:
	}
}

func (g *ComfortNoiseGenerator) generateComfortNoiseUlaw(samples int) []byte {
	payload := make([]byte, samples)
	for index := range payload {
		g.seed = g.seed*1103515245 + 12345
		sample := int16((g.seed>>16)&0x1ff) - 0x100
		payload[index] = linearToUlaw(sample)
	}
	return payload
}

func linearToUlaw(sample int16) byte {
	const bias = 0x84
	sign := byte(0)
	value := int(sample)
	if value < 0 {
		sign = 0x80
		value = -value
	}
	if value > 32635 {
		value = 32635
	}
	value += bias
	exponent := 7
	for mask := 0x4000; exponent > 0 && value&mask == 0; mask >>= 1 {
		exponent--
	}
	mantissa := (value >> (exponent + 3)) & 0x0f
	return ^(sign | byte(exponent<<4) | byte(mantissa))
}
