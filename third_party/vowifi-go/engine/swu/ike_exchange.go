package swu

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

// sendIKE records the request that receiveIKE must retransmit while waiting
// for the matching response.
func (s *Session) sendIKE(raw []byte) error {
	if s.socket == nil {
		return errors.New("swu: no IKE transport")
	}
	packet, err := ikev2.DecodePacket(raw)
	if err != nil {
		return fmt.Errorf("swu: encode IKE request: %w", err)
	}
	s.mu.Lock()
	s.lastIKERequest = append(s.lastIKERequest[:0], raw...)
	s.lastIKEMessageID = packet.MessageID
	s.mu.Unlock()
	s.socket.SendIKE(raw)
	return nil
}

// receiveIKE waits for the response matching the most recent request and
// retransmits that exact request according to the recovered RFC 7296 policy.
func (s *Session) receiveIKE(ctx context.Context) (*ikev2.IKEPacket, error) {
	request, expected, err := s.pendingIKERequest()
	if err != nil {
		return nil, err
	}
	policy := normalizedRetransmitConfig(s.cfg)
	delay := policy.InitialDelay
	for retries := 0; ; retries++ {
		packet, timedOut, err := s.waitForIKEResponse(ctx, expected, delay)
		if err != nil || !timedOut {
			return packet, err
		}
		if retries >= policy.MaxRetries {
			return nil, ErrTaskTimeout
		}
		s.socket.SendIKE(request)
		delay = time.Duration(float64(delay) * policy.Backoff)
	}
}

func (s *Session) pendingIKERequest() ([]byte, *ikev2.IKEPacket, error) {
	if s.socket == nil {
		return nil, nil, errors.New("swu: no IKE transport")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.lastIKERequest) == 0 {
		return nil, nil, errors.New("swu: no pending IKE request")
	}
	raw := append([]byte(nil), s.lastIKERequest...)
	request, err := ikev2.DecodePacket(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("swu: decode pending IKE request: %w", err)
	}
	return raw, request, nil
}

func (s *Session) waitForIKEResponse(ctx context.Context, expected *ikev2.IKEPacket, delay time.Duration) (*ikev2.IKEPacket, bool, error) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-timer.C:
			return nil, true, nil
		case raw, ok := <-s.socket.IKEPackets():
			if !ok {
				return nil, false, errors.New("swu: IKE transport closed")
			}
			packet, err := ikev2.DecodePacket(raw)
			if err != nil {
				return nil, false, err
			}
			if validIKEResponseHeader(packet, expected) {
				return packet, false, nil
			}
		}
	}
}

func validIKEResponseHeader(packet, request *ikev2.IKEPacket) bool {
	if packet == nil || request == nil {
		return false
	}
	if packet.MessageID != request.MessageID || packet.ExchangeType != request.ExchangeType {
		return false
	}
	if packet.Flags&0x20 == 0 || packet.Flags&0x08 != 0 {
		return false
	}
	if packet.InitiatorSPI != request.InitiatorSPI {
		return false
	}
	return request.ResponderSPI == ([8]byte{}) || packet.ResponderSPI == request.ResponderSPI
}

func normalizedRetransmitConfig(cfg *Config) RetransmitConfig {
	policy := DefaultRetransmitConfig
	if cfg != nil && cfg.Retransmit != nil {
		policy = *cfg.Retransmit
	}
	if policy.MaxRetries < 0 {
		policy.MaxRetries = 0
	}
	if policy.InitialDelay <= 0 {
		policy.InitialDelay = DefaultRetransmitConfig.InitialDelay
	}
	if policy.Backoff < 1 {
		policy.Backoff = 1
	}
	return policy
}
