package swu

import (
	"fmt"
	"time"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

// Stop is idempotent and returns after pending and queued transactions close.
func (tm *TaskManager) Stop() {
	if tm == nil {
		return
	}
	tm.stopOnce.Do(tm.cancel)
	<-tm.done
}

// EnqueueRequest restores the original full-transaction API.
func (tm *TaskManager) EnqueueRequest(
	msgID uint32,
	exchange ikev2.ExchangeType,
	payloads []ikev2.Payload,
	packets [][]byte,
) <-chan []byte {
	return tm.enqueue(msgID, exchange, payloads, packets).CompletionCh
}

// EnqueueRawRequest retains the interim raw request API with explicit errors.
func (tm *TaskManager) EnqueueRawRequest(msgID uint32, message []byte) <-chan TaskResponse {
	return tm.enqueue(msgID, 0, nil, [][]byte{message}).resultCh
}

func (tm *TaskManager) enqueue(
	msgID uint32,
	exchange ikev2.ExchangeType,
	payloads []ikev2.Payload,
	packets [][]byte,
) *OutgoingMessage {
	message := &OutgoingMessage{
		MsgID: msgID, Exchange: exchange, Payloads: append([]ikev2.Payload(nil), payloads...),
		Packets: clonePacketSet(packets), MaxRetries: tm.config.MaxRetries,
		NextTimeout: tm.config.InitialTimeout, CompletionCh: make(chan []byte, 1),
		resultCh: make(chan TaskResponse, 1),
	}
	tm.mu.Lock()
	switch {
	case tm.stopped:
		tm.failMessage(message, ErrTaskManagerStopped)
	case tm.containsMessageID(msgID):
		tm.failMessage(message, ErrDuplicateMessageID)
	case len(tm.pending) < tm.windowSize:
		tm.activateMessage(message)
	default:
		tm.queue = append(tm.queue, message)
	}
	tm.mu.Unlock()
	return message
}

func clonePacketSet(packets [][]byte) [][]byte {
	cloned := make([][]byte, len(packets))
	for index := range packets {
		cloned[index] = append([]byte(nil), packets[index]...)
	}
	return cloned
}

func (tm *TaskManager) containsMessageID(msgID uint32) bool {
	if _, ok := tm.pending[msgID]; ok {
		return true
	}
	for _, message := range tm.queue {
		if message.MsgID == msgID {
			return true
		}
	}
	return false
}

// HandleResponse restores the original response dispatch API.
func (tm *TaskManager) HandleResponse(msgID uint32, response []byte) bool {
	return tm.handleResponse(msgID, 0, false, response)
}

func (tm *TaskManager) handleResponseForExchange(
	msgID uint32,
	exchange ikev2.ExchangeType,
	response []byte,
) bool {
	return tm.handleResponse(msgID, exchange, true, response)
}

func (tm *TaskManager) handleResponse(
	msgID uint32,
	exchange ikev2.ExchangeType,
	checkExchange bool,
	response []byte,
) bool {
	tm.mu.Lock()
	message, ok := tm.pending[msgID]
	if !ok || (checkExchange && message.Exchange != exchange) {
		tm.mu.Unlock()
		return false
	}
	delete(tm.pending, msgID)
	message.completed = true
	data := append([]byte(nil), response...)
	message.CompletionCh <- data
	message.resultCh <- TaskResponse{Message: append([]byte(nil), data...)}
	close(message.resultCh)
	tm.pumpQueue()
	tm.mu.Unlock()
	return true
}

func (tm *TaskManager) cancelRequest(msgID uint32, cause error) {
	tm.mu.Lock()
	if message, ok := tm.pending[msgID]; ok {
		delete(tm.pending, msgID)
		tm.failMessage(message, cause)
		tm.pumpQueue()
		tm.mu.Unlock()
		return
	}
	for index, message := range tm.queue {
		if message.MsgID != msgID {
			continue
		}
		tm.queue = append(tm.queue[:index], tm.queue[index+1:]...)
		tm.failMessage(message, cause)
		break
	}
	tm.mu.Unlock()
}

func (tm *TaskManager) activateMessage(message *OutgoingMessage) {
	message.Deadline = time.Now().Add(message.NextTimeout)
	tm.pending[message.MsgID] = message
	tm.sendMessage(message)
	select {
	case tm.wakeupCh <- struct{}{}:
	default:
	}
}

func (tm *TaskManager) sendMessage(message *OutgoingMessage) {
	var err error
	if tm.sendFunc != nil {
		err = tm.sendFunc(message.Packets)
	} else if tm.sendRaw != nil {
		for _, packet := range message.Packets {
			if err = tm.sendRaw(message.MsgID, packet); err != nil {
				break
			}
		}
	}
	if err != nil {
		message.lastSendErr = err
	}
}

func (tm *TaskManager) pumpQueue() {
	if len(tm.queue) == 0 || len(tm.pending) >= tm.windowSize {
		return
	}
	next := tm.queue[0]
	tm.queue = tm.queue[1:]
	tm.activateMessage(next)
}

func taskTimeoutError(sendErr error) error {
	if sendErr == nil {
		return ErrWindowTimeout
	}
	return fmt.Errorf("%w: last send failed: %w", ErrWindowTimeout, sendErr)
}
