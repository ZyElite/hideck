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
	_, err := ikev2.DecodePacket(raw)
	if err != nil {
		return fmt.Errorf("swu: encode IKE request: %w", err)
	}
	s.mu.Lock()
	s.lastIKERequest = append(s.lastIKERequest[:0], raw...)
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
	decoded := s.controlResponseSource()
	var rawPackets <-chan []byte
	if decoded == nil {
		rawPackets = s.socket.IKEPackets()
	}
	for {
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-timer.C:
			return nil, true, nil
		case packet, ok := <-decoded:
			if !ok {
				return nil, false, errors.New("swu: IKE control loop closed")
			}
			if validIKEResponseHeader(packet, expected) {
				return packet, false, nil
			}
		case raw, ok := <-rawPackets:
			if !ok {
				return nil, false, errors.New("swu: IKE transport closed")
			}
			packet, err := ikev2.DecodePacket(raw)
			if err != nil {
				return nil, false, err
			}
			if validIKEResponseHeader(packet, expected) {
				s.mu.Lock()
				s.lastIKEResponse = append(s.lastIKEResponse[:0], raw...)
				s.mu.Unlock()
				return packet, false, nil
			}
		}
	}
}

func (s *Session) controlResponseSource() <-chan *ikev2.IKEPacket {
	s.controlMu.RLock()
	defer s.controlMu.RUnlock()
	if !s.controlRunning {
		return nil
	}
	return s.controlResponses
}

func validIKEResponseHeader(packet, request *ikev2.IKEPacket) bool {
	if packet == nil || request == nil {
		return false
	}
	packetHeader := packetIKEHeader(packet)
	requestHeader := packetIKEHeader(request)
	if packetHeader.MessageID != requestHeader.MessageID || packetHeader.ExchangeType != requestHeader.ExchangeType {
		return false
	}
	if packetHeader.Flags&ikeResponseFlag == 0 ||
		packetHeader.Flags&ikeInitiatorFlag == requestHeader.Flags&ikeInitiatorFlag {
		return false
	}
	if packetHeader.SPIi != requestHeader.SPIi {
		return false
	}
	return requestHeader.SPIr == 0 || packetHeader.SPIr == requestHeader.SPIr
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
