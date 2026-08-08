package imscore

import (
	"errors"
	"sync"
	"time"
)

type memoryDeliveryPart struct {
	messageID string
	partNo    int
	callID    string
	rpMR      int
	state     string
	sipCode   int
	rpCause   int
	errorText string
}

type memoryDeliveryStore struct {
	mu         sync.Mutex
	deliveries map[string]*DeliveryStatus
	parts      map[string][]*memoryDeliveryPart
}

func newMemoryDeliveryStore() *memoryDeliveryStore {
	return &memoryDeliveryStore{
		deliveries: make(map[string]*DeliveryStatus),
		parts:      make(map[string][]*memoryDeliveryPart),
	}
}

func (s *memoryDeliveryStore) CreateSMSDelivery(messageID, imsi, deviceID, peer, content string, partsTotal int, _ time.Time) error {
	s.mu.Lock()
	s.deliveries[messageID] = &DeliveryStatus{
		MessageID: messageID, IMSI: imsi, DeviceID: deviceID,
		Peer: peer, Content: content, PartsTotal: partsTotal, State: smsDeliveryStatePending,
	}
	s.mu.Unlock()
	return nil
}

func (s *memoryDeliveryStore) UpsertSMSDeliveryPart(messageID string, partNo int, callID string, rpMR int, state string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, part := range s.parts[messageID] {
		if part.partNo == partNo {
			part.callID, part.rpMR, part.state = callID, rpMR, state
			return nil
		}
	}
	s.parts[messageID] = append(s.parts[messageID], &memoryDeliveryPart{
		messageID: messageID, partNo: partNo, callID: callID, rpMR: rpMR, state: state,
	})
	return nil
}

func (s *memoryDeliveryStore) MarkSMSDeliveryPartSIPResult(
	messageID string,
	partNo, sipCode int,
	state, errText string,
	_ time.Time,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, part := range s.parts[messageID] {
		if part.partNo == partNo {
			part.state, part.sipCode, part.errorText = state, sipCode, errText
			return nil
		}
	}
	return errors.New("delivery part not found")
}

func (s *memoryDeliveryStore) MarkSMSDeliveryPartReport(inReplyTo, callID, _ string, rpMR int, state string, sipCode int, rpCause int, errText string, _ time.Time) (DeliveryPartMatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	part := s.findPart(inReplyTo, callID, rpMR)
	if part == nil {
		return DeliveryPartMatch{}, errors.New("delivery part not found")
	}
	part.state = state
	if part.sipCode == 0 && sipCode > 0 {
		part.sipCode = sipCode
	}
	part.rpCause, part.errorText = rpCause, errText
	return DeliveryPartMatch{
		MessageID: part.messageID, PartNo: part.partNo, State: state, Matched: true,
	}, nil
}

func (s *memoryDeliveryStore) findPart(inReplyTo, callID string, rpMR int) *memoryDeliveryPart {
	for _, parts := range s.parts {
		for _, part := range parts {
			if inReplyTo != "" && part.callID == inReplyTo {
				return part
			}
			if callID != "" && part.callID == callID {
				return part
			}
			if part.rpMR == rpMR {
				return part
			}
		}
	}
	return nil
}

func (s *memoryDeliveryStore) RecomputeSMSDelivery(messageID string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delivery := s.deliveries[messageID]
	if delivery == nil {
		return errors.New("delivery not found")
	}
	acked, failed := 0, false
	for _, part := range s.parts[messageID] {
		if part.state == smsDeliveryStateAcked {
			acked++
		}
		if part.state == smsDeliveryStateFailed || part.state == smsDeliveryPartStateTimeout {
			failed = true
			delivery.LastError = part.errorText
		}
	}
	delivery.Acks = acked
	switch {
	case failed:
		delivery.State = smsDeliveryStateFailed
	case acked == delivery.PartsTotal:
		delivery.State = smsDeliveryStateAcked
	case acked > 0:
		delivery.State = "partial_ack"
	default:
		delivery.State = smsDeliveryStatePending
	}
	return nil
}

func (s *memoryDeliveryStore) UpdateSMSDeliveryState(messageID, state, lastError string, acks int, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delivery := s.deliveries[messageID]
	if delivery == nil {
		return errors.New("delivery not found")
	}
	delivery.State, delivery.LastError, delivery.Acks = state, lastError, acks
	return nil
}

func (s *memoryDeliveryStore) GetSMSDeliveryStatus(messageID string) (*DeliveryStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delivery := s.deliveries[messageID]
	if delivery == nil {
		return nil, errors.New("delivery not found")
	}
	copyStatus := *delivery
	copyStatus.Parts = make([]DeliveryPartStatus, 0, len(s.parts[messageID]))
	for _, part := range s.parts[messageID] {
		copyStatus.Parts = append(copyStatus.Parts, DeliveryPartStatus{
			PartNo: part.partNo, CallID: part.callID, State: part.state,
			SIPCode: part.sipCode, RPCause: part.rpCause,
		})
	}
	return &copyStatus, nil
}

func (s *memoryDeliveryStore) part(messageID string, partNo int) memoryDeliveryPart {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, part := range s.parts[messageID] {
		if part.partNo == partNo {
			return *part
		}
	}
	return memoryDeliveryPart{}
}
