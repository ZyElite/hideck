package mbim

import (
	"errors"
	"time"
)

const defaultFragmentTimeout = 1250 * time.Millisecond

type fragmentAssembly struct {
	collector  *collector
	timer      *time.Timer
	indication bool
}

func (d *Device) handleCommandDoneFragment(tx uint32, message []byte) {
	response, done, err := d.collectFragment(tx, message, false)
	if err != nil {
		d.rejectFragment(tx, err)
		return
	}
	if done {
		d.deliver(tx, MessageTypeCommandDone, response)
	}
}

func (d *Device) handleIndicationFragment(tx uint32, message []byte) {
	response, done, err := d.collectFragment(tx, commandDoneShapeIndication(message), true)
	if err != nil {
		d.rejectFragment(tx, err)
		return
	}
	if !done {
		return
	}
	select {
	case d.indications <- Indication{Service: response.Service, CID: response.CID, InfoBuffer: response.InfoBuffer}:
	default:
		d.reportProtocolError(errors.New("mbim: indication queue is full"))
	}
}

func (d *Device) collectFragment(
	tx uint32,
	message []byte,
	indication bool,
) (CommandDone, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	assembly := d.collector[tx]
	if assembly == nil {
		assembly = &fragmentAssembly{collector: newCollector(), indication: indication}
		d.collector[tx] = assembly
	} else {
		current, hasFragmentHeader := fragmentNumber(message)
		if assembly.indication != indication || (hasFragmentHeader && current == 0) {
			return CommandDone{}, false, &ProtocolError{
				Code:          ProtocolErrorDuplicatedTID,
				TransactionID: tx,
				Detail:        "new message reused a transaction that is still being reassembled",
			}
		}
	}

	done, err := assembly.collector.add(message)
	if err != nil {
		d.removeCollectorLocked(tx)
		return CommandDone{}, false, withProtocolTransaction(err, tx)
	}
	if !done {
		d.armFragmentTimerLocked(tx, assembly)
		return CommandDone{}, false, nil
	}
	response, err := assembly.collector.commandDone()
	d.removeCollectorLocked(tx)
	if err != nil {
		return CommandDone{}, false, withProtocolTransaction(err, tx)
	}
	return response, true, nil
}

func (d *Device) armFragmentTimerLocked(tx uint32, assembly *fragmentAssembly) {
	if assembly.timer != nil {
		assembly.timer.Stop()
	}
	timeout := d.fragmentTimeout
	if timeout <= 0 {
		timeout = defaultFragmentTimeout
	}
	assembly.timer = time.AfterFunc(timeout, func() { d.expireFragment(tx, assembly) })
}

func (d *Device) expireFragment(tx uint32, expected *fragmentAssembly) {
	d.mu.Lock()
	current := d.collector[tx]
	if current != expected {
		d.mu.Unlock()
		return
	}
	delete(d.collector, tx)
	pending, hasPending := d.pending[tx]
	if hasPending {
		delete(d.pending, tx)
	}
	d.mu.Unlock()

	err := &ProtocolError{
		Code:          ProtocolErrorTimeoutFragment,
		TransactionID: tx,
		Detail:        "time between fragments exceeded 1250ms",
	}
	if sendErr := d.sendHostError(tx, ProtocolErrorTimeoutFragment); sendErr != nil {
		err.Detail += "; HOST_ERROR send failed: " + sendErr.Error()
	}
	d.reportProtocolError(err)
	if hasPending {
		pending.result <- commandResult{err: err}
	}
}

func (d *Device) rejectFragment(tx uint32, err error) {
	protocolErr := withProtocolTransaction(err, tx)
	if sendErr := d.sendHostError(tx, protocolErr.Code); sendErr != nil {
		protocolErr.Detail += "; HOST_ERROR send failed: " + sendErr.Error()
	}
	d.reportProtocolError(protocolErr)
	if protocolErr.Code == ProtocolErrorDuplicatedTID {
		return
	}
	d.deliverError(tx, protocolErr)
}

func fragmentNumber(message []byte) (uint32, bool) {
	if len(message) < headerLen+fragHdrLen {
		return 0, false
	}
	return le.Uint32(message[headerLen+4:]), true
}

func (d *Device) removeCollectorLocked(tx uint32) {
	assembly := d.collector[tx]
	if assembly != nil && assembly.timer != nil {
		assembly.timer.Stop()
	}
	delete(d.collector, tx)
}

func withProtocolTransaction(err error, tx uint32) *ProtocolError {
	var protocolErr *ProtocolError
	if errors.As(err, &protocolErr) {
		return &ProtocolError{Code: protocolErr.Code, TransactionID: tx, Detail: protocolErr.Detail}
	}
	return &ProtocolError{Code: ProtocolErrorUnknown, TransactionID: tx, Detail: err.Error()}
}

// commandDoneShapeIndication inserts a zero status field so the shared
// collector can reassemble INDICATE_STATUS fragments.
func commandDoneShapeIndication(message []byte) []byte {
	if len(message) < headerLen+fragHdrLen {
		return message
	}
	current := le.Uint32(message[headerLen+4:])
	if current != 0 {
		return message
	}
	if len(message) < headerLen+fragHdrLen+uuidLen+4+4 {
		return message
	}

	output := make([]byte, len(message)+4)
	copy(output[:headerLen+fragHdrLen+uuidLen+4], message[:headerLen+fragHdrLen+uuidLen+4])
	le.PutUint32(output[4:], uint32(len(output)))
	copy(output[headerLen+fragHdrLen+uuidLen+4+4:], message[headerLen+fragHdrLen+uuidLen+4:])
	return output
}
