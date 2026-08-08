package swu

import "time"

func (tm *TaskManager) windowLoop() {
	ticker := time.NewTicker(tm.checkInterval)
	defer ticker.Stop()
	defer close(tm.done)
	for {
		select {
		case <-tm.ctx.Done():
			tm.failAll(ErrTaskManagerStopped)
			return
		case <-tm.wakeupCh:
			tm.checkTimeouts()
		case <-ticker.C:
			tm.checkTimeouts()
		}
	}
}

func (tm *TaskManager) failAll(cause error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.stopped = true
	for _, message := range tm.pending {
		tm.failMessage(message, cause)
	}
	for _, message := range tm.queue {
		tm.failMessage(message, cause)
	}
	tm.pending = make(map[uint32]*OutgoingMessage)
	tm.queue = nil
}

func (tm *TaskManager) checkTimeouts() {
	now := time.Now()
	tm.mu.Lock()
	for id, message := range tm.pending {
		if now.Before(message.Deadline) {
			continue
		}
		if message.RetryCount >= message.MaxRetries {
			delete(tm.pending, id)
			tm.failMessage(message, taskTimeoutError(message.lastSendErr))
			continue
		}
		message.RetryCount++
		message.NextTimeout = time.Duration(float64(message.NextTimeout) * tm.config.BackoffFactor)
		if tm.config.MaxTimeout > 0 && message.NextTimeout > tm.config.MaxTimeout {
			message.NextTimeout = tm.config.MaxTimeout
		}
		message.Deadline = now.Add(message.NextTimeout)
		tm.sendMessage(message)
	}
	for len(tm.queue) > 0 && len(tm.pending) < tm.windowSize {
		tm.pumpQueue()
	}
	tm.mu.Unlock()
}

func (tm *TaskManager) failMessage(message *OutgoingMessage, cause error) {
	if message.completed {
		return
	}
	message.completed = true
	if !message.isClosed {
		close(message.CompletionCh)
		message.isClosed = true
	}
	message.resultCh <- TaskResponse{Err: cause}
	close(message.resultCh)
}
