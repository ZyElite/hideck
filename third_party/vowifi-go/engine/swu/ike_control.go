package swu

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
)

const (
	defaultIKEWindowSize = 5
	ikeControlQueueSize  = 8
)

type ikeWaitKey struct {
	exchangeType ikev2.ExchangeType
	msgID        uint32
}

// startIKEControl starts the single IKE socket reader and peer-request worker.
func (s *Session) startIKEControl() error {
	if s.socket == nil {
		return errors.New("swu: no IKE transport")
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	s.controlMu.Lock()
	if s.controlRunning {
		s.controlMu.Unlock()
		return nil
	}
	transport := s.socket
	requests := make(chan []byte, ikeControlQueueSize)
	taskManager := s.newSessionTaskManager(transport)
	s.controlRequests = requests
	s.taskMgr = taskManager
	s.controlRunning = true
	s.controlWG.Add(2)
	s.controlMu.Unlock()
	go s.ikeDispatchLoop()
	go s.ikeRequestLoop(requests)
	return nil
}

func (s *Session) newSessionTaskManager(transport ipsec.Transport) *TaskManager {
	config := s.cfg.IKERetryConfig
	interval := taskManagerPollInterval
	if config == nil && s.cfg.Retransmit != nil {
		config, interval = convertRetransmitConfig(s.cfg.Retransmit)
	}
	return newTaskManager(
		s.ctx, s.cfg.DeviceID, config, defaultIKEWindowSize,
		func(packets [][]byte) error { return sendIKEPacketSet(transport, packets) },
		nil, interval,
	)
}

func sendIKEPacketSet(transport ipsec.Transport, packets [][]byte) error {
	if transport == nil {
		return errors.New("swu: no IKE transport")
	}
	if len(packets) == 0 {
		return errors.New("swu: empty IKE packet set")
	}
	for _, packet := range packets {
		if len(packet) == 0 {
			return errors.New("swu: empty IKE packet")
		}
		if err := transport.SendIKE(packet); err != nil {
			return fmt.Errorf("swu: send IKE packet set: %w", err)
		}
	}
	return nil
}

// ensureIKEDispatcher restores the original idempotent dispatcher entrypoint.
func (s *Session) ensureIKEDispatcher() {
	if err := s.startIKEControl(); err != nil {
		s.failEstablishedControl(err)
	}
}

// startIKEControlLoop restores the original established-control entrypoint.
func (s *Session) startIKEControlLoop() {
	s.ensureIKEDispatcher()
}

// ikeDispatchLoop restores the original no-argument socket-reader symbol.
func (s *Session) ikeDispatchLoop() {
	s.controlMu.RLock()
	transport := s.socket
	taskManager := s.taskMgr
	requests := s.controlRequests
	s.controlMu.RUnlock()
	s.runIKEDispatchLoop(transport, taskManager, requests)
}

func (s *Session) runIKEDispatchLoop(
	transport ipsec.Transport,
	taskManager *TaskManager,
	requests chan<- []byte,
) {
	defer s.controlWG.Done()
	defer taskManager.Stop()
	defer close(requests)
	defer func() {
		s.controlMu.Lock()
		if s.taskMgr == taskManager {
			s.controlRunning = false
		}
		s.controlMu.Unlock()
	}()
	for {
		select {
		case <-s.ctx.Done():
			return
		case raw, ok := <-transport.IKEPackets():
			if !ok {
				s.failEstablishedControl(errors.New("swu: IKE transport closed"))
				return
			}
			if err := s.dispatchIKEPacket(raw, taskManager, requests); err != nil {
				s.failEstablishedControl(err)
				return
			}
		}
	}
}

func (s *Session) dispatchIKEPacket(
	raw []byte,
	taskManager *TaskManager,
	requests chan<- []byte,
) error {
	header, err := ikev2.DecodeHeader(raw)
	if err != nil {
		return fmt.Errorf("swu: decode established IKE header: %w", err)
	}
	if err := s.validateEstablishedIKEEnvelope(header, len(raw)); err != nil {
		return err
	}
	if header.Flags&ikeResponseFlag != 0 {
		return s.dispatchIKEResponse(header, raw, taskManager)
	}
	select {
	case requests <- append([]byte(nil), raw...):
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

func (s *Session) validateEstablishedIKEEnvelope(header *ikev2.IKEHeader, rawLength int) error {
	if header.Version>>4 != 2 {
		return fmt.Errorf("swu: unsupported IKE version 0x%02x", header.Version)
	}
	if header.Length != uint32(rawLength) {
		return fmt.Errorf("swu: IKE length %d does not match datagram %d", header.Length, rawLength)
	}
	s.mu.RLock()
	initiatorSPI := binary.BigEndian.Uint64(s.SPIi[:])
	responderSPI := binary.BigEndian.Uint64(s.SPIr[:])
	localInitiator := s.localIKEInitiator
	s.mu.RUnlock()
	if header.SPIi != initiatorSPI || header.SPIr != responderSPI {
		return errors.New("swu: established IKE packet has unexpected SPIs")
	}
	peerInitiator := header.Flags&ikeInitiatorFlag != 0
	if peerInitiator == localInitiator {
		return errors.New("swu: established IKE packet has invalid initiator flag")
	}
	return nil
}

func (s *Session) dispatchIKEResponse(
	header *ikev2.IKEHeader,
	raw []byte,
	taskManager *TaskManager,
) error {
	if taskManager.handleResponseForExchange(header.MessageID, header.ExchangeType, raw) {
		return nil
	}
	key := ikeWaitKey{exchangeType: header.ExchangeType, msgID: header.MessageID}
	s.controlMu.Lock()
	waiter := s.ikeWaiters[key]
	if waiter == nil {
		s.ikePending[key] = append([]byte(nil), raw...)
		s.controlMu.Unlock()
		return nil
	}
	s.controlMu.Unlock()
	select {
	case waiter <- append([]byte(nil), raw...):
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

func (s *Session) ikeRequestLoop(requests <-chan []byte) {
	defer s.controlWG.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case raw, ok := <-requests:
			if !ok {
				return
			}
			if err := s.handleIncomingIKE(raw); err != nil {
				s.failEstablishedControl(err)
				return
			}
		}
	}
}

func (s *Session) handleIncomingIKE(raw []byte) error {
	header, err := ikev2.DecodeHeader(raw)
	if err != nil {
		return fmt.Errorf("swu: decode peer IKE header: %w", err)
	}
	switch header.ExchangeType {
	case ikev2.ExchangeInformational:
		return s.handleIncomingInformational(raw)
	case ikev2.ExchangeCreateChildSA:
		s.dispatchCreateChildSA(raw)
		return nil
	default:
		return fmt.Errorf("swu: unsupported established IKE exchange %d", header.ExchangeType)
	}
}

// handleIncomingInformational restores the original raw-packet boundary.
func (s *Session) handleIncomingInformational(raw []byte) error {
	packet, err := ikev2.DecodePacket(raw)
	if err != nil {
		return fmt.Errorf("swu: decode peer INFORMATIONAL: %w", err)
	}
	return s.handleIncomingInformationalPacket(packet)
}

// dispatchCreateChildSA restores the original raw CREATE_CHILD_SA dispatcher.
func (s *Session) dispatchCreateChildSA(raw []byte) {
	packet, err := ikev2.DecodePacket(raw)
	if err == nil {
		err = s.handleIncomingCreateChildSAPacket(packet)
	}
	if err != nil {
		s.failEstablishedControl(fmt.Errorf("swu: handle peer CREATE_CHILD_SA: %w", err))
	}
}

func (s *Session) failEstablishedControl(err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	s.cancel()
	s.stopTimers()
	s.setTerminalError(err)
	s.signalDone()
}
