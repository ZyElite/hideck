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
	return s.sendIKERequestPackets([][]byte{raw})
}

func (s *Session) sendIKERequestPackets(packets [][]byte) error {
	if s.socket == nil {
		return errors.New("swu: no IKE transport")
	}
	if len(packets) == 0 {
		return errors.New("swu: empty IKE request packet set")
	}
	if _, err := validateIKERequestPacketSet(packets); err != nil {
		return err
	}
	s.mu.Lock()
	s.lastIKERequest = append(s.lastIKERequest[:0], packets[0]...)
	s.lastIKERequestSet = clonePacketSet(packets)
	s.mu.Unlock()
	if err := sendIKEPacketSet(s.socket, packets); err != nil {
		return err
	}
	return nil
}

func validateIKERequestPacketSet(packets [][]byte) (*ikev2.IKEHeader, error) {
	var first *ikev2.IKEHeader
	for index, raw := range packets {
		header, err := ikev2.DecodeHeader(raw)
		if err != nil {
			return nil, fmt.Errorf("swu: invalid IKE request packet %d: %w", index, err)
		}
		if header.Length != uint32(len(raw)) {
			return nil, fmt.Errorf(
				"swu: IKE request packet %d length %d does not match %d",
				index, header.Length, len(raw),
			)
		}
		if header.Flags&ikeResponseFlag != 0 {
			return nil, fmt.Errorf("swu: IKE request packet %d has response flag", index)
		}
		if first == nil {
			first = header
			continue
		}
		if header.SPIi != first.SPIi || header.SPIr != first.SPIr ||
			header.ExchangeType != first.ExchangeType || header.MessageID != first.MessageID ||
			header.Flags != first.Flags {
			return nil, errors.New("swu: IKE request packet set has inconsistent headers")
		}
	}
	return first, nil
}

// receiveIKE waits for the response matching the most recent request and
// retransmits that exact request according to the recovered RFC 7296 policy.
func (s *Session) receiveIKE(ctx context.Context) (*ikev2.IKEPacket, error) {
	requests, expected, err := s.pendingIKERequest()
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
			s.fragmentBuf.drop(packetIKEHeader(expected).MessageID)
			return nil, ErrTaskTimeout
		}
		if err := sendIKEPacketSet(s.socket, requests); err != nil {
			return nil, fmt.Errorf("swu: retransmit IKE request set: %w", err)
		}
		delay = time.Duration(float64(delay) * policy.Backoff)
	}
}

func (s *Session) pendingIKERequest() ([][]byte, *ikev2.IKEPacket, error) {
	if s.socket == nil {
		return nil, nil, errors.New("swu: no IKE transport")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.lastIKERequestSet) == 0 {
		return nil, nil, errors.New("swu: no pending IKE request")
	}
	packets := clonePacketSet(s.lastIKERequestSet)
	header, err := ikev2.DecodeHeader(packets[0])
	if err != nil {
		return nil, nil, fmt.Errorf("swu: decode pending IKE request: %w", err)
	}
	return packets, &ikev2.IKEPacket{Header: header}, nil
}

func (s *Session) waitForIKEResponse(ctx context.Context, expected *ikev2.IKEPacket, delay time.Duration) (*ikev2.IKEPacket, bool, error) {
	if s.ikeControlIsRunning() {
		return s.waitForDispatchedIKEResponse(ctx, expected, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	rawPackets := s.socket.IKEPackets()
	for {
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-timer.C:
			return nil, true, nil
		case raw, ok := <-rawPackets:
			if !ok {
				return nil, false, errors.New("swu: IKE transport closed")
			}
			header, err := ikev2.DecodeHeader(raw)
			if err != nil {
				return nil, false, err
			}
			if !validIKEResponseHeaderFields(header, packetIKEHeader(expected)) {
				continue
			}
			normalized, complete, err := s.normalizeInboundIKE(raw)
			if err != nil {
				return nil, false, err
			}
			if !complete {
				continue
			}
			packet, err := ikev2.DecodePacket(normalized)
			if err != nil {
				return nil, false, err
			}
			if validIKEResponseHeader(packet, expected) {
				s.mu.Lock()
				s.lastIKEResponse = append(s.lastIKEResponse[:0], normalized...)
				s.mu.Unlock()
				return packet, false, nil
			}
		}
	}
}

func (s *Session) ikeControlIsRunning() bool {
	s.controlMu.RLock()
	defer s.controlMu.RUnlock()
	return s.controlRunning
}

func (s *Session) waitForDispatchedIKEResponse(
	ctx context.Context,
	expected *ikev2.IKEPacket,
	delay time.Duration,
) (*ikev2.IKEPacket, bool, error) {
	header := packetIKEHeader(expected)
	raw, timedOut, err := s.waitForDispatchedIKERaw(
		ctx, ikeWaitKey{exchangeType: header.ExchangeType, msgID: header.MessageID}, delay,
	)
	if err != nil || timedOut {
		return nil, timedOut, err
	}
	packet, err := ikev2.DecodePacket(raw)
	if err != nil {
		return nil, false, err
	}
	if !validIKEResponseHeader(packet, expected) {
		return nil, false, errors.New("swu: dispatched IKE response header mismatch")
	}
	s.mu.Lock()
	s.lastIKEResponse = append(s.lastIKEResponse[:0], raw...)
	s.mu.Unlock()
	return packet, false, nil
}

func validIKEResponseHeader(packet, request *ikev2.IKEPacket) bool {
	if packet == nil || request == nil {
		return false
	}
	packetHeader := packetIKEHeader(packet)
	requestHeader := packetIKEHeader(request)
	return validIKEResponseHeaderFields(packetHeader, requestHeader)
}

func validIKEResponseHeaderFields(packetHeader, requestHeader *ikev2.IKEHeader) bool {
	if packetHeader == nil || requestHeader == nil ||
		packetHeader.MessageID != requestHeader.MessageID ||
		packetHeader.ExchangeType != requestHeader.ExchangeType {
		return false
	}
	if packetHeader.Flags&ikeResponseFlag == 0 ||
		packetHeader.Flags&ikeInitiatorFlag == requestHeader.Flags&ikeInitiatorFlag ||
		packetHeader.SPIi != requestHeader.SPIi {
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
