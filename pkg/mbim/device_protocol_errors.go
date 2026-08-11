package mbim

import (
	"context"
	"errors"
	"fmt"
)

func (d *Device) sendHostError(tx uint32, code ProtocolErrorCode) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	d.mu.Lock()
	closeSent := d.closeSent || d.closed
	d.mu.Unlock()
	if closeSent {
		return fmt.Errorf("mbim: cannot send HOST_ERROR after CLOSE")
	}
	return d.tr.WriteMessage(encodeProtocolError(MessageTypeHostError, tx, code))
}

func (d *Device) reportProtocolError(err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	d.protocolErrorStart.Do(func() { go d.runProtocolErrorQueue() })
	select {
	case d.protocolErrorInput <- err:
	case <-d.protocolErrorDone:
	}
}

func (d *Device) runProtocolErrorQueue() {
	defer close(d.protocolErrors)
	var queue []error
	for {
		var output chan error
		var next error
		if len(queue) > 0 {
			output = d.protocolErrors
			next = queue[0]
		}
		select {
		case err := <-d.protocolErrorInput:
			queue = append(queue, err)
		case output <- next:
			queue[0] = nil
			queue = queue[1:]
		case <-d.protocolErrorDone:
			return
		}
	}
}
