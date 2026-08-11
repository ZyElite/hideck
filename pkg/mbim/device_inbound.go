package mbim

import (
	"errors"
	"fmt"
)

func (d *Device) readLoop() {
	for {
		message, err := d.tr.ReadMessage()
		if err != nil {
			d.markReadFailure(fmt.Errorf("mbim: read message: %w", err))
			return
		}
		d.handleInboundMessage(message)
	}
}

func (d *Device) handleInboundMessage(message []byte) {
	header, err := decodeHeader(message)
	if err != nil {
		var protocolErr *ProtocolError
		if len(message) >= headerLen && errors.As(err, &protocolErr) {
			d.rejectInbound(header, protocolErr.Code, protocolErr.Detail)
			return
		}
		d.reportProtocolError(err)
		return
	}
	switch header.Type {
	case MessageTypeOpenDone, MessageTypeCloseDone:
		d.handleLifecycleResponse(header, message)
	case MessageTypeCommandDone:
		d.handleCommandDoneFragment(header.TransactionID, message)
	case MessageTypeIndicateStatus:
		d.handleIndication(header, message)
	case MessageTypeFunctionError:
		d.handleFunctionError(header, message)
	default:
		d.rejectInbound(header, ProtocolErrorUnknown, fmt.Sprintf("unexpected device message type 0x%08x", header.Type))
	}
}

func (d *Device) handleLifecycleResponse(header Header, message []byte) {
	if len(message) != headerLen+4 {
		d.rejectInbound(header, ProtocolErrorLengthMismatch, "lifecycle response must be 16 bytes")
		return
	}
	d.deliver(header.TransactionID, header.Type, CommandDone{Status: le.Uint32(message[headerLen:])})
}

func (d *Device) handleIndication(header Header, message []byte) {
	if header.TransactionID != 0 {
		d.rejectInbound(header, ProtocolErrorUnknown, "INDICATE_STATUS transaction ID must be zero")
		return
	}
	d.handleIndicationFragment(header.TransactionID, message)
}

func (d *Device) handleFunctionError(header Header, message []byte) {
	err := decodeFunctionError(header, message)
	d.reportProtocolError(err)
	d.deliverError(header.TransactionID, err)
}

func (d *Device) rejectInbound(header Header, code ProtocolErrorCode, detail string) {
	err := &ProtocolError{Code: code, TransactionID: header.TransactionID, Detail: detail}
	if sendErr := d.sendHostError(header.TransactionID, code); sendErr != nil {
		err.Detail += "; HOST_ERROR send failed: " + sendErr.Error()
	}
	d.reportProtocolError(err)
	d.deliverError(header.TransactionID, err)
}

func (d *Device) removePending(tx uint32) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, exists := d.pending[tx]
	delete(d.pending, tx)
	d.removeCollectorLocked(tx)
	return exists
}

func (d *Device) deliver(tx uint32, actual MessageType, response CommandDone) {
	pending, exists := d.takePending(tx)
	if !exists {
		return
	}
	if pending.expectation.messageType != actual {
		d.rejectResponseType(tx, pending, actual)
		return
	}
	if commandResponseMismatch(pending.expectation, response) {
		d.rejectCommandResponse(tx, pending, response)
		return
	}
	pending.result <- commandResult{resp: response}
}

func (d *Device) takePending(tx uint32) (pendingTransaction, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	pending, exists := d.pending[tx]
	if exists {
		delete(d.pending, tx)
		d.removeCollectorLocked(tx)
	}
	return pending, exists
}

func (d *Device) rejectResponseType(tx uint32, pending pendingTransaction, actual MessageType) {
	d.rejectResponse(tx, pending, &ProtocolError{
		Code:          ProtocolErrorUnknown,
		TransactionID: tx,
		Detail: fmt.Sprintf(
			"response type 0x%08x does not match expected 0x%08x",
			actual,
			pending.expectation.messageType,
		),
	})
}

func commandResponseMismatch(expectation transactionExpectation, response CommandDone) bool {
	return expectation.command &&
		(!expectation.service.Equal(response.Service) || expectation.cid != response.CID)
}

func (d *Device) rejectCommandResponse(tx uint32, pending pendingTransaction, response CommandDone) {
	d.rejectResponse(tx, pending, &ProtocolError{
		Code:          ProtocolErrorUnknown,
		TransactionID: tx,
		Detail: fmt.Sprintf(
			"COMMAND_DONE service=%s cid=%d does not match request service=%s cid=%d",
			response.Service.String(),
			response.CID,
			pending.expectation.service.String(),
			pending.expectation.cid,
		),
	})
}

func (d *Device) rejectResponse(tx uint32, pending pendingTransaction, err *ProtocolError) {
	if sendErr := d.sendHostError(tx, err.Code); sendErr != nil {
		err.Detail += "; HOST_ERROR send failed: " + sendErr.Error()
	}
	d.reportProtocolError(err)
	pending.result <- commandResult{err: err}
}

func (d *Device) deliverError(tx uint32, err error) {
	pending, exists := d.takePending(tx)
	if exists {
		pending.result <- commandResult{err: err}
	}
}

func (d *Device) markReadFailure(err error) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.readErr = err
	d.mu.Unlock()
	d.reportProtocolError(err)
	d.failPending(err)
}

func (d *Device) failPending(err error) {
	d.mu.Lock()
	pending := d.pending
	d.pending = make(map[uint32]pendingTransaction)
	for tx := range d.collector {
		d.removeCollectorLocked(tx)
	}
	d.mu.Unlock()
	for _, transaction := range pending {
		transaction.result <- commandResult{err: err}
	}
}
