package pcsc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

type Channel struct {
	service           *Service
	selector          Selector
	resetOnDisconnect bool
	activeContext     atomic.Pointer[context.Context]
	mu                sync.Mutex
	session           *Session
	channel           byte
}

func NewChannel(service *Service, selector Selector, resetOnDisconnect bool) *Channel {
	return &Channel{service: service, selector: selector, resetOnDisconnect: resetOnDisconnect}
}

func (channel *Channel) SetContext(ctx context.Context) { channel.activeContext.Store(&ctx) }

func (channel *Channel) CurrentChannel() byte {
	channel.mu.Lock()
	defer channel.mu.Unlock()
	return channel.channel
}

func (channel *Channel) context() context.Context {
	if current := channel.activeContext.Load(); current != nil {
		return *current
	}
	return context.Background()
}

func (channel *Channel) Connect() error {
	channel.mu.Lock()
	defer channel.mu.Unlock()
	if channel.session != nil {
		return nil
	}
	session, err := channel.service.OpenSession(channel.context(), channel.selector)
	if err != nil {
		return err
	}
	channel.session = session
	return nil
}

func (channel *Channel) Disconnect() error {
	channel.mu.Lock()
	defer channel.mu.Unlock()
	return channel.disconnectLocked()
}

func (channel *Channel) disconnectLocked() error {
	if channel.session == nil {
		return nil
	}
	var err error
	if channel.resetOnDisconnect {
		err = channel.session.CloseWithReset()
	} else {
		err = channel.session.Close()
	}
	channel.session = nil
	channel.channel = 0
	return err
}

func (channel *Channel) OpenLogicalChannel(aid []byte) (byte, error) {
	channel.mu.Lock()
	defer channel.mu.Unlock()
	if channel.session == nil {
		return 0, errors.New("pcsc: channel is not connected")
	}
	if len(aid) == 0 || len(aid) > 255 {
		return 0, errors.New("pcsc: logical channel AID must contain 1 to 255 bytes")
	}
	data, sw, err := channel.session.Transmit(channel.context(), []byte{0x00, 0x70, 0x00, 0x00, 0x01})
	if err != nil || sw != 0x9000 || len(data) != 1 {
		closeErr := channel.disconnectLocked()
		if err != nil {
			return 0, errors.Join(fmt.Errorf("pcsc: open logical channel: %w", err), closeErr)
		}
		return 0, errors.Join(fmt.Errorf("pcsc: open logical channel failed with status %04X", sw), closeErr)
	}
	logical := data[0]
	if logical == 0 || logical >= 20 {
		return 0, errors.Join(fmt.Errorf("pcsc: invalid logical channel %d", logical), channel.disconnectLocked())
	}
	selectAPDU := []byte{logicalChannelCLA(0x00, logical), 0xA4, 0x04, 0x00, byte(len(aid))}
	selectAPDU = append(selectAPDU, aid...)
	selectAPDU = append(selectAPDU, 0x00)
	_, sw, err = channel.session.Transmit(channel.context(), selectAPDU)
	if err != nil || sw != 0x9000 {
		closeErr := errors.Join(channel.closeLogicalChannelLocked(logical), channel.disconnectLocked())
		if err != nil {
			return 0, errors.Join(fmt.Errorf("pcsc: select logical channel application: %w", err), closeErr)
		}
		return 0, errors.Join(logicalChannelSelectError(sw), closeErr)
	}
	channel.channel = logical
	return logical, nil
}

func logicalChannelSelectError(status uint16) error {
	err := fmt.Errorf("pcsc: select logical channel application failed with status %04X", status)
	if status == 0x6A82 {
		return errors.Join(ErrApplicationNotFound, err)
	}
	return err
}

func (channel *Channel) Transmit(command []byte) ([]byte, error) {
	channel.mu.Lock()
	defer channel.mu.Unlock()
	if channel.session == nil || channel.channel == 0 {
		return nil, errors.New("pcsc: logical channel is not open")
	}
	if len(command) < 4 {
		return nil, errors.New("pcsc: APDU command is shorter than its header")
	}
	command = append([]byte(nil), command...)
	command[0] = logicalChannelCLA(command[0], channel.channel)
	data, sw, err := channel.session.Transmit(channel.context(), command)
	if err != nil {
		return nil, err
	}
	return append(data, byte(sw>>8), byte(sw)), nil
}

func (channel *Channel) CloseLogicalChannel(logical byte) error {
	channel.mu.Lock()
	defer channel.mu.Unlock()
	return channel.closeLogicalChannelLocked(logical)
}

func (channel *Channel) closeLogicalChannelLocked(logical byte) error {
	if channel.session == nil || logical == 0 {
		return nil
	}
	_, sw, err := channel.session.Transmit(channel.context(), []byte{0x00, 0x70, 0x80, logical, 0x00})
	channel.channel = 0
	if err != nil {
		return fmt.Errorf("pcsc: close logical channel: %w", err)
	}
	if sw != 0x9000 {
		return fmt.Errorf("pcsc: close logical channel failed with status %04X", sw)
	}
	return nil
}

func logicalChannelCLA(class, channel byte) byte {
	if channel < 4 {
		return class&0xFC | channel
	}
	return class&0xB0 | 0x40 | (channel - 4)
}
