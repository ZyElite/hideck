package mbim

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	minimumMaxControlTransfer = 64
	defaultOpenAttemptTimeout = 5 * time.Second
	defaultCloseTimeout       = 5 * time.Second
	defaultProxyOpenTimeout   = 45 * time.Second
)

var errOpenAttemptTimeout = errors.New("mbim: OPEN attempt timed out")

// Open performs the MBIM OPEN state transition. A timed-out attempt is retried
// with a new transaction ID until the caller's total deadline expires.
func (d *Device) Open(ctx context.Context, maxControlTransfer uint32) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxControlTransfer < minimumMaxControlTransfer {
		return fmt.Errorf("mbim: MaxControlTransfer=%d, minimum is %d", maxControlTransfer, minimumMaxControlTransfer)
	}
	if err := d.beginOpen(maxControlTransfer); err != nil {
		return err
	}
	succeeded := false
	defer func() { d.finishOpen(succeeded) }()

	if err := d.configureProxy(ctx); err != nil {
		return err
	}
	for {
		err := d.openAttempt(ctx, maxControlTransfer)
		if errors.Is(err, errOpenAttemptTimeout) {
			continue
		}
		if err != nil {
			return err
		}
		succeeded = true
		return nil
	}
}

func (d *Device) beginOpen(maxControlTransfer uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return fmt.Errorf("mbim: device transport is closed")
	}
	if d.readErr != nil {
		return d.readErr
	}
	if d.opened || d.opening {
		return fmt.Errorf("mbim: device is already open or opening")
	}
	d.opening = true
	d.maxCtrl = maxControlTransfer
	if !d.readStarted {
		d.readStarted = true
		go d.readLoop()
	}
	return nil
}

func (d *Device) finishOpen(succeeded bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.opening = false
	if !d.closed {
		d.opened = succeeded
	}
}

func (d *Device) configureProxy(ctx context.Context) error {
	pc, ok := d.tr.(proxyConfigurer)
	if !ok {
		return nil
	}
	path, needed := pc.needsProxyConfig()
	if !needed {
		return nil
	}
	info := encodeProxyConfigInfo(path, remainingTimeoutSeconds(ctx, defaultProxyOpenTimeout))
	if _, err := d.command(ctx, UUIDProxyControl, CIDProxyControlConfiguration, CommandTypeSet, info, false); err != nil {
		return fmt.Errorf("mbim: proxy configuration: %w", err)
	}
	return nil
}

func remainingTimeoutSeconds(ctx context.Context, fallback time.Duration) uint32 {
	remaining := fallback
	if deadline, ok := ctx.Deadline(); ok {
		remaining = time.Until(deadline)
	}
	if remaining <= 0 {
		return 1
	}
	seconds := (remaining + time.Second - 1) / time.Second
	if seconds > time.Duration(^uint32(0))*time.Second {
		return ^uint32(0)
	}
	return uint32(seconds / time.Second)
}

func (d *Device) openAttempt(ctx context.Context, maxControlTransfer uint32) error {
	expectation := transactionExpectation{messageType: MessageTypeOpenDone}
	tx, ch, _, err := d.beginTransaction(expectation, false)
	if err != nil {
		return err
	}
	if err := d.writeMessage(encodeOpen(tx, maxControlTransfer)); err != nil {
		d.removePending(tx)
		return fmt.Errorf("mbim: send OPEN: %w", err)
	}
	timer := time.NewTimer(d.openAttemptTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		d.removePending(tx)
		return ctx.Err()
	case <-timer.C:
		d.removePending(tx)
		return errOpenAttemptTimeout
	case result := <-ch:
		if result.err != nil {
			return result.err
		}
		if result.resp.Status != 0 {
			return &StatusError{Op: "OPEN", Status: result.resp.Status}
		}
		return nil
	}
}

// CloseContext completes CLOSE/CLOSE_DONE before releasing the transport.
func (d *Device) CloseContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	d.closeMu.Lock()
	defer d.closeMu.Unlock()

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	canHandshake := d.opened && d.readErr == nil
	if canHandshake {
		d.closing = true
	}
	d.mu.Unlock()
	if !canHandshake {
		return d.closeTransport(nil)
	}
	cancelErr := d.cancelCommandsBeforeClose()

	expectation := transactionExpectation{messageType: MessageTypeCloseDone}
	tx, ch, _, err := d.beginTransaction(expectation, false)
	if err != nil {
		return d.closeTransport(errors.Join(cancelErr, err))
	}
	if err := d.writeCloseMessage(encodeClose(tx)); err != nil {
		d.removePending(tx)
		return d.closeTransport(errors.Join(cancelErr, fmt.Errorf("mbim: send CLOSE: %w", err)))
	}

	var protocolErr error
	select {
	case <-ctx.Done():
		d.removePending(tx)
		protocolErr = ctx.Err()
	case result := <-ch:
		protocolErr = result.err
		if protocolErr == nil && result.resp.Status != 0 {
			protocolErr = &StatusError{Op: "CLOSE", Status: result.resp.Status}
		}
	}
	return d.closeTransport(errors.Join(cancelErr, protocolErr))
}

func (d *Device) writeCloseMessage(message []byte) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	d.mu.Lock()
	d.closeSent = true
	d.mu.Unlock()
	return d.tr.WriteMessage(message)
}

type closingCommand struct {
	transactionID uint32
	result        chan commandResult
}

func (d *Device) cancelCommandsBeforeClose() error {
	d.mu.Lock()
	commands := make([]closingCommand, 0, len(d.pending))
	for tx, pending := range d.pending {
		if !pending.expectation.command {
			continue
		}
		delete(d.pending, tx)
		d.removeCollectorLocked(tx)
		commands = append(commands, closingCommand{transactionID: tx, result: pending.result})
	}
	d.mu.Unlock()

	var sendErrors []error
	for _, command := range commands {
		commandErr := fmt.Errorf("mbim: command tx=%d canceled because device is closing", command.transactionID)
		if err := d.sendHostError(command.transactionID, ProtocolErrorCancel); err != nil {
			sendErrors = append(sendErrors, fmt.Errorf("cancel command tx=%d: %w", command.transactionID, err))
		}
		command.result <- commandResult{err: commandErr}
	}
	return errors.Join(sendErrors...)
}

// Close uses the standard MBIM close timeout and is idempotent.
func (d *Device) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultCloseTimeout)
	defer cancel()
	return d.CloseContext(ctx)
}

func (d *Device) closeTransport(protocolErr error) error {
	d.mu.Lock()
	d.closed = true
	d.opened = false
	d.opening = false
	d.closing = false
	d.mu.Unlock()
	d.failPending(fmt.Errorf("mbim: device closed"))
	d.writeMu.Lock()
	transportErr := d.tr.Close()
	d.writeMu.Unlock()
	d.protocolErrorStop.Do(func() { close(d.protocolErrorDone) })
	return errors.Join(protocolErr, transportErr)
}
