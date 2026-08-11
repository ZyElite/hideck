package mbim

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Device is an opened MBIM control endpoint with transaction multiplexing.
type Device struct {
	tr Transport

	mu          sync.Mutex
	writeMu     sync.Mutex
	closeMu     sync.Mutex
	nextTx      uint32
	pending     map[uint32]pendingTransaction
	maxCtrl     uint32
	opened      bool
	opening     bool
	closing     bool
	closeSent   bool
	closed      bool
	readStarted bool
	readErr     error
	collector   map[uint32]*fragmentAssembly

	openAttemptTimeout time.Duration
	fragmentTimeout    time.Duration
	indications        chan Indication
	protocolErrors     chan error
	protocolErrorInput chan error
	protocolErrorDone  chan struct{}
	protocolErrorStart sync.Once
	protocolErrorStop  sync.Once
}

type pendingTransaction struct {
	expectation transactionExpectation
	result      chan commandResult
}

type transactionExpectation struct {
	messageType MessageType
	service     UUID
	cid         uint32
	command     bool
}

type commandResult struct {
	resp CommandDone
	err  error
}

// Indication is an unsolicited INDICATE_STATUS message.
type Indication struct {
	Service    UUID
	CID        uint32
	InfoBuffer []byte
}

// NewDevice constructs an MBIM device client around an established transport.
func NewDevice(tr Transport) *Device {
	return newDevice(tr)
}

func newDevice(tr Transport) *Device {
	return &Device{
		tr:                 tr,
		pending:            make(map[uint32]pendingTransaction),
		collector:          make(map[uint32]*fragmentAssembly),
		openAttemptTimeout: defaultOpenAttemptTimeout,
		fragmentTimeout:    defaultFragmentTimeout,
		indications:        make(chan Indication, 16),
		protocolErrors:     make(chan error),
		protocolErrorInput: make(chan error),
		protocolErrorDone:  make(chan struct{}),
	}
}

// Command sends a COMMAND and waits for the matching COMMAND_DONE response.
func (d *Device) Command(ctx context.Context, service UUID, cid uint32, ct CommandType, info []byte) (CommandDone, error) {
	return d.command(ctx, service, cid, ct, info, true)
}

func (d *Device) command(
	ctx context.Context,
	service UUID,
	cid uint32,
	commandType CommandType,
	info []byte,
	requireOpened bool,
) (CommandDone, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	expectation := transactionExpectation{
		messageType: MessageTypeCommandDone,
		service:     service,
		cid:         cid,
		command:     true,
	}
	tx, ch, maxCtrl, err := d.beginTransaction(expectation, requireOpened)
	if err != nil {
		return CommandDone{}, err
	}
	if err := d.writeCommandMessages(tx, splitCommand(tx, service, cid, commandType, info, maxCtrl)); err != nil {
		d.removePending(tx)
		return CommandDone{}, fmt.Errorf("mbim: send COMMAND: %w", err)
	}

	select {
	case <-ctx.Done():
		if !d.removePending(tx) {
			return CommandDone{}, ctx.Err()
		}
		if cancelErr := d.sendHostError(tx, ProtocolErrorCancel); cancelErr != nil {
			return CommandDone{}, fmt.Errorf("%w; send MBIM CANCEL: %v", ctx.Err(), cancelErr)
		}
		return CommandDone{}, ctx.Err()
	case result := <-ch:
		return result.resp, result.err
	}
}

func (d *Device) beginTransaction(
	expectation transactionExpectation,
	requireOpened bool,
) (uint32, <-chan commandResult, uint32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return 0, nil, 0, fmt.Errorf("mbim: device transport is closed")
	}
	if d.readErr != nil {
		return 0, nil, 0, d.readErr
	}
	if requireOpened && (!d.opened || d.closing) {
		return 0, nil, 0, fmt.Errorf("mbim: device is not opened")
	}
	if !requireOpened && !d.opening && !d.closing {
		return 0, nil, 0, fmt.Errorf("mbim: lifecycle transaction outside open/close")
	}
	tx, err := d.allocateTransactionIDLocked()
	if err != nil {
		return 0, nil, 0, err
	}
	result := make(chan commandResult, 1)
	d.pending[tx] = pendingTransaction{expectation: expectation, result: result}
	return tx, result, d.maxCtrl, nil
}

func (d *Device) allocateTransactionIDLocked() (uint32, error) {
	for attempts := 0; attempts <= len(d.pending); attempts++ {
		d.nextTx++
		if d.nextTx == 0 {
			d.nextTx = 1
		}
		if _, exists := d.pending[d.nextTx]; !exists {
			return d.nextTx, nil
		}
	}
	return 0, fmt.Errorf("mbim: no transaction ID available")
}

func (d *Device) writeMessages(messages [][]byte) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	for _, message := range messages {
		if err := d.tr.WriteMessage(message); err != nil {
			return err
		}
	}
	return nil
}

func (d *Device) writeCommandMessages(tx uint32, messages [][]byte) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	for index, message := range messages {
		if err := d.tr.WriteMessage(message); err != nil {
			return err
		}
		if index < len(messages)-1 && !d.hasPending(tx) {
			return nil
		}
	}
	return nil
}

func (d *Device) hasPending(tx uint32) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, exists := d.pending[tx]
	return exists
}

func (d *Device) writeMessage(message []byte) error {
	return d.writeMessages([][]byte{message})
}

// Indications returns unsolicited INDICATE_STATUS messages.
func (d *Device) Indications() <-chan Indication {
	return d.indications
}

// ProtocolErrors exposes asynchronous MBIM wire errors, including tx=0 errors.
func (d *Device) ProtocolErrors() <-chan error {
	return d.protocolErrors
}
