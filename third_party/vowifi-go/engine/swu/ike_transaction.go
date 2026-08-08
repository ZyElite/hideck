package swu

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

// receiveIKEWithTimeout restores the legacy fixed IKE_SA_INIT waiter.
func (s *Session) receiveIKEWithTimeout(timeout time.Duration) ([]byte, error) {
	return s.receiveIKEResponseWithTimeout(ikev2.IKE_SA_INIT, 0, timeout)
}

func (s *Session) receiveIKEResponseWithTimeout(
	exchangeType ikev2.ExchangeType,
	msgID uint32,
	timeout time.Duration,
) ([]byte, error) {
	if err := s.startIKEControl(); err != nil {
		return nil, err
	}
	key := ikeWaitKey{exchangeType: exchangeType, msgID: msgID}
	data, timedOut, err := s.waitForDispatchedIKERaw(s.ctx, key, timeout)
	if timedOut {
		return nil, context.DeadlineExceeded
	}
	return data, err
}

func (s *Session) waitForDispatchedIKERaw(
	ctx context.Context,
	key ikeWaitKey,
	timeout time.Duration,
) ([]byte, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	waiter := make(chan []byte, 1)
	s.controlMu.Lock()
	if pending, ok := s.ikePending[key]; ok {
		delete(s.ikePending, key)
		s.controlMu.Unlock()
		return pending, false, nil
	}
	if _, exists := s.ikeWaiters[key]; exists {
		s.controlMu.Unlock()
		return nil, false, errors.New("swu: duplicate IKE response waiter")
	}
	s.ikeWaiters[key] = waiter
	s.controlMu.Unlock()
	defer s.removeIKEWaiter(key, waiter)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-s.ctx.Done():
		return nil, false, s.ctx.Err()
	case <-ctx.Done():
		return nil, false, ctx.Err()
	case <-timer.C:
		return nil, true, nil
	case data := <-waiter:
		return data, false, nil
	}
}

func (s *Session) removeIKEWaiter(key ikeWaitKey, waiter chan []byte) {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	if s.ikeWaiters[key] == waiter {
		delete(s.ikeWaiters, key)
	}
}

// sendEncryptedWithRetry restores the production sliding-window exchange.
func (s *Session) sendEncryptedWithRetry(
	payloads []ikev2.Payload,
	exchangeType ikev2.ExchangeType,
) ([]byte, error) {
	return s.sendEncryptedWithRetryContext(s.ctx, payloads, exchangeType)
}

func (s *Session) sendEncryptedWithRetryContext(
	ctx context.Context,
	payloads []ikev2.Payload,
	exchangeType ikev2.ExchangeType,
) ([]byte, error) {
	if s.shouldFragment(payloads) {
		packets, err := s.fragmentMessage(payloads, exchangeType)
		if err != nil {
			return nil, err
		}
		if len(packets) == 0 {
			return nil, errors.New("swu: fragmentation selected but produced no SKF packets")
		}
		header, err := ikev2.DecodeHeader(packets[0])
		if err != nil {
			return nil, err
		}
		request := &ikev2.IKEPacket{Header: header, Payloads: payloads}
		return s.exchangeEstablishedRaw(ctx, request, packets)
	}
	packet := &ikev2.IKEPacket{
		Header: newIKEHeader(
			s.SPIi, s.SPIr, exchangeType, s.localIKEFlags(false), s.nextMessageID(),
		),
		Payloads: payloads,
	}
	raw, err := s.encryptAndWrap(packet)
	if err != nil {
		return nil, err
	}
	return s.exchangeEstablishedRaw(ctx, packet, [][]byte{raw})
}

func (s *Session) exchangeEstablishedRaw(
	ctx context.Context,
	request *ikev2.IKEPacket,
	packets [][]byte,
) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(packets) == 0 {
		return nil, errors.New("swu: empty IKE packet set")
	}
	if err := s.startIKEControl(); err != nil {
		return nil, err
	}
	header := packetIKEHeader(request)
	s.mu.Lock()
	s.lastIKERequest = append(s.lastIKERequest[:0], packets[0]...)
	s.lastIKERequestSet = clonePacketSet(packets)
	s.mu.Unlock()
	s.controlMu.RLock()
	taskManager := s.taskMgr
	s.controlMu.RUnlock()
	if taskManager == nil {
		return nil, errors.New("swu: IKE task manager is not initialized")
	}
	message := taskManager.enqueue(header.MessageID, header.ExchangeType, request.Payloads, packets)
	select {
	case <-ctx.Done():
		taskManager.cancelRequest(header.MessageID, ctx.Err())
		s.fragmentBuf.drop(header.MessageID)
		return nil, ctx.Err()
	case result := <-message.resultCh:
		response, err := s.acceptEstablishedResult(result)
		if err != nil {
			s.fragmentBuf.drop(header.MessageID)
		}
		return response, err
	}
}

func (s *Session) acceptEstablishedResult(result TaskResponse) ([]byte, error) {
	if result.Err != nil {
		return nil, result.Err
	}
	if len(result.Message) == 0 {
		return nil, errors.New("swu: empty IKE response")
	}
	s.mu.Lock()
	s.lastIKEResponse = append(s.lastIKEResponse[:0], result.Message...)
	s.mu.Unlock()
	return result.Message, nil
}

// sendEncryptedResponseWithMsgID restores the explicit-exchange response API.
func (s *Session) sendEncryptedResponseWithMsgID(
	payloads []ikev2.Payload,
	exchangeType ikev2.ExchangeType,
	msgID uint32,
) error {
	transport := s.transport()
	if transport == nil {
		return errors.New("swu: no IKE transport")
	}
	if s.shouldFragmentWithFlags(payloads, s.localIKEFlags(true)) {
		packets, err := s.fragmentResponse(payloads, exchangeType, msgID)
		if err != nil {
			return err
		}
		if len(packets) == 0 {
			return errors.New("swu: response fragmentation produced no SKF packets")
		}
		return sendIKEPacketSet(transport, packets)
	}
	packet := &ikev2.IKEPacket{
		Header: newIKEHeader(
			s.SPIi, s.SPIr, exchangeType, s.localIKEFlags(true), msgID,
		),
		Payloads: payloads,
	}
	raw, err := s.encryptAndWrapWithMsgID(packet, msgID)
	if err != nil {
		return err
	}
	if err := transport.SendIKE(raw); err != nil {
		return fmt.Errorf("swu: send IKE response: %w", err)
	}
	return nil
}
