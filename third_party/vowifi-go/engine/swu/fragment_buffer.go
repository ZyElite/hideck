package swu

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

const maxFragmentedMessageSize = 64 * 1024

// fragmentBuffer preserves the original Message-ID keyed reassembly layout.
type fragmentBuffer struct {
	mu    sync.Mutex
	frags map[uint32]*fragmentSet
}

type fragmentSet struct {
	total    uint16
	received map[uint16][]byte
	totalLen int

	firstPayload    ikev2.PayloadType
	hasFirstPayload bool
	envelope        fragmentEnvelope
	hasEnvelope     bool
}

type fragmentEnvelope struct {
	initiatorSPI uint64
	responderSPI uint64
	exchangeType ikev2.ExchangeType
	flags        uint8
	version      uint8
}

type receivedFragment struct {
	messageID    uint32
	number       uint16
	total        uint16
	firstPayload ikev2.PayloadType
	plaintext    []byte
	envelope     *fragmentEnvelope
}

func newFragmentBuffer() *fragmentBuffer {
	return &fragmentBuffer{frags: make(map[uint32]*fragmentSet)}
}

// addFragment restores the original buffer API.
func (fb *fragmentBuffer) addFragment(
	msgID uint32,
	fragNum uint16,
	totalFrags uint16,
	plaintext []byte,
) (bool, error) {
	return fb.addReceivedFragment(receivedFragment{
		messageID: msgID, number: fragNum, total: totalFrags, plaintext: plaintext,
	})
}

func (fb *fragmentBuffer) addReceivedFragment(fragment receivedFragment) (bool, error) {
	if fragment.number == 0 || fragment.total == 0 ||
		fragment.number > fragment.total || fragment.total > maxFragments {
		return false, fmt.Errorf("invalid IKE fragment %d/%d", fragment.number, fragment.total)
	}
	if fragment.number > 1 && fragment.firstPayload != ikev2.NoNextPayload {
		return false, errors.New("non-initial IKE fragment declares a next payload")
	}
	fb.mu.Lock()
	defer fb.mu.Unlock()
	set := fb.frags[fragment.messageID]
	if set == nil {
		set = newFragmentSet(fragment.total)
		fb.frags[fragment.messageID] = set
	}
	if fragment.total > set.total {
		set = newFragmentSet(fragment.total)
		fb.frags[fragment.messageID] = set
	} else if fragment.total != set.total {
		return false, fmt.Errorf("fragment total mismatch: have %d, got %d", set.total, fragment.total)
	}
	if err := set.acceptMetadata(fragment); err != nil {
		return false, err
	}
	if existing, duplicate := set.received[fragment.number]; duplicate {
		if !bytes.Equal(existing, fragment.plaintext) {
			return false, fmt.Errorf(
				"conflicting duplicate IKE fragment %d/%d", fragment.number, fragment.total,
			)
		}
		return false, nil
	}
	if set.totalLen+len(fragment.plaintext) > maxFragmentedMessageSize {
		delete(fb.frags, fragment.messageID)
		return false, fmt.Errorf(
			"fragmented message %d exceeds maximum size %d",
			fragment.messageID, maxFragmentedMessageSize,
		)
	}
	set.received[fragment.number] = append([]byte(nil), fragment.plaintext...)
	set.totalLen += len(fragment.plaintext)
	return len(set.received) == int(set.total), nil
}

func (set *fragmentSet) acceptMetadata(fragment receivedFragment) error {
	if fragment.envelope != nil {
		if set.hasEnvelope && set.envelope != *fragment.envelope {
			return errors.New("IKE fragment envelope does not match its fragment set")
		}
		set.envelope, set.hasEnvelope = *fragment.envelope, true
	}
	if fragment.number != 1 {
		return nil
	}
	if set.hasFirstPayload && set.firstPayload != fragment.firstPayload {
		return errors.New("first IKE fragment declares a conflicting next payload")
	}
	set.firstPayload, set.hasFirstPayload = fragment.firstPayload, true
	return nil
}

func newFragmentSet(total uint16) *fragmentSet {
	return &fragmentSet{total: total, received: make(map[uint16][]byte)}
}

// reassemble restores the original ordered, consuming API.
func (fb *fragmentBuffer) reassemble(msgID uint32) ([]byte, error) {
	data, _, err := fb.reassembleWithFirst(msgID)
	return data, err
}

func (fb *fragmentBuffer) reassembleWithFirst(msgID uint32) ([]byte, ikev2.PayloadType, error) {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	set := fb.frags[msgID]
	if set == nil {
		return nil, 0, errors.New("no fragments for message")
	}
	result := make([]byte, 0, set.totalLen)
	for number := uint16(1); number <= set.total; number++ {
		fragment, ok := set.received[number]
		if !ok {
			return nil, 0, fmt.Errorf("missing fragment %d of %d for message %d", number, set.total, msgID)
		}
		result = append(result, fragment...)
	}
	delete(fb.frags, msgID)
	return result, set.firstPayload, nil
}

func (fb *fragmentBuffer) drop(msgID uint32) {
	fb.mu.Lock()
	delete(fb.frags, msgID)
	fb.mu.Unlock()
}

func (fb *fragmentBuffer) clear() {
	fb.mu.Lock()
	clear(fb.frags)
	fb.mu.Unlock()
}
