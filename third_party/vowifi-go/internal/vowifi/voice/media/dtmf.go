package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	dtmfPacketInterval       = 20 * time.Millisecond
	dtmfMinimum              = 40 * time.Millisecond
	dtmfDefaultClockRate     = 8000
	dtmfEndPacketCount       = 3
	dtmfVolume               = 10
	dtmfDefaultEventMask     = uint16(0xffff)
	dtmfMaximumDurationUnits = uint64(1<<16 - 1)
)

const dtmfDigits = "0123456789*#ABCD"

type dtmfSendPlan struct {
	remote        *net.UDPAddr
	event         byte
	steps         int
	finalDuration uint16
	payloadType   int
	clockRate     int
	timestamp     uint32
	ssrc          uint32
	eventEndAt    time.Time
}

type dtmfPacket struct {
	plan     dtmfSendPlan
	duration uint16
	marker   bool
	end      bool
}

func (r *RTPRelay) seedDTMF(source io.Reader) {
	if source == nil {
		r.dtmfMu.Lock()
		r.dtmfSeedErr = errors.New("media: initialize DTMF RTP source: nil random reader")
		r.dtmfMu.Unlock()
		return
	}
	seed := make([]byte, 10)
	_, err := io.ReadFull(source, seed)
	r.dtmfMu.Lock()
	defer r.dtmfMu.Unlock()
	if err != nil {
		r.dtmfSeedErr = fmt.Errorf("media: initialize DTMF RTP source: %w", err)
		return
	}
	r.dtmfSequence = binary.BigEndian.Uint16(seed[:2])
	r.dtmfTimestamp = binary.BigEndian.Uint32(seed[2:6])
	r.dtmfSSRC = binary.BigEndian.Uint32(seed[6:10])
	r.dtmfSeedErr = nil
}

// SetDTMFPayloadType configures the common telephone-event/8000 form.
func (r *RTPRelay) SetDTMFPayloadType(payloadType int) error {
	return r.ConfigureDTMF(payloadType, dtmfDefaultClockRate, "")
}

// ConfigureDTMF stores the negotiated telephone-event format and event set.
func (r *RTPRelay) ConfigureDTMF(payloadType, clockRate int, events string) error {
	if r == nil {
		return errors.New("media: nil relay")
	}
	if payloadType < 0 || payloadType > 127 {
		return fmt.Errorf("media: invalid DTMF payload type %d", payloadType)
	}
	if clockRate <= 0 {
		return fmt.Errorf("media: invalid DTMF clock rate %d", clockRate)
	}
	eventMask, err := parseDTMFEventMask(events)
	if err != nil {
		return err
	}
	r.dtmfMu.Lock()
	r.dtmfPayloadType = payloadType
	r.dtmfClockRate = clockRate
	r.dtmfEventMask = eventMask
	r.dtmfMu.Unlock()
	return nil
}

// DisableDTMF clears a prior telephone-event negotiation.
func (r *RTPRelay) DisableDTMF() {
	if r == nil {
		return
	}
	r.dtmfMu.Lock()
	r.dtmfPayloadType = -1
	r.dtmfMu.Unlock()
}

func parseDTMFEventMask(events string) (uint16, error) {
	events = strings.TrimSpace(events)
	if events == "" {
		return dtmfDefaultEventMask, nil
	}
	if strings.ContainsAny(events, " \t\r\n") {
		return 0, errors.New("media: DTMF event list contains whitespace")
	}
	var mask uint16
	for _, token := range strings.Split(events, ",") {
		start, end, err := parseDTMFEventRange(token)
		if err != nil {
			return 0, err
		}
		for event := start; event <= end && event < 16; event++ {
			mask |= uint16(1) << event
		}
	}
	return mask, nil
}

func parseDTMFEventRange(token string) (int, int, error) {
	parts := strings.Split(token, "-")
	if len(parts) < 1 || len(parts) > 2 || parts[0] == "" {
		return 0, 0, fmt.Errorf("media: invalid DTMF event range %q", token)
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("media: invalid DTMF event range %q", token)
	}
	end := start
	if len(parts) == 2 {
		end, err = strconv.Atoi(parts[1])
	}
	if err != nil || start < 0 || end < start || end > 255 {
		return 0, 0, fmt.Errorf("media: invalid DTMF event range %q", token)
	}
	return start, end, nil
}

// SendDTMF sends one RFC 4733 event to the negotiated IMS RTP peer.
func (r *RTPRelay) SendDTMF(digit rune, duration time.Duration) error {
	if r == nil {
		return errors.New("media: nil relay")
	}
	if err := r.beginDTMFSend(); err != nil {
		return err
	}
	defer r.dtmfWG.Done()
	r.dtmfSendMu.Lock()
	defer r.dtmfSendMu.Unlock()
	plan, err := r.prepareDTMFSend(digit, duration)
	if err != nil {
		return err
	}
	plan = r.startDTMFEvent(plan)
	defer r.finishDTMFSend(plan)
	return r.sendDTMFEvent(plan)
}

func (r *RTPRelay) beginDTMFSend() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.active || r.isStopped() {
		return errors.New("media: RTP relay is not active")
	}
	r.dtmfWG.Add(1)
	return nil
}

func (r *RTPRelay) prepareDTMFSend(digit rune, duration time.Duration) (dtmfSendPlan, error) {
	eventIndex := strings.IndexRune(dtmfDigits, digit)
	if eventIndex < 0 {
		return dtmfSendPlan{}, fmt.Errorf("media: unsupported DTMF digit %q", digit)
	}
	if r.isStopped() {
		return dtmfSendPlan{}, errors.New("media: RTP relay is not active")
	}
	remote := r.remoteAddr.Load()
	if r.connIMS == nil || remote == nil {
		return dtmfSendPlan{}, errors.New("media: IMS RTP destination is unavailable")
	}
	return r.prepareNegotiatedDTMF(remote, byte(eventIndex), duration)
}

func (r *RTPRelay) prepareNegotiatedDTMF(
	remote *net.UDPAddr,
	event byte,
	duration time.Duration,
) (dtmfSendPlan, error) {
	r.dtmfMu.Lock()
	defer r.dtmfMu.Unlock()
	if r.dtmfPayloadType < 0 {
		return dtmfSendPlan{}, errors.New("media: telephone-event payload type was not negotiated")
	}
	if r.dtmfEventMask&(uint16(1)<<event) == 0 {
		return dtmfSendPlan{}, fmt.Errorf("media: DTMF event %d was not negotiated", event)
	}
	steps, finalDuration, err := normalizeDTMFDuration(duration, r.dtmfClockRate)
	if err != nil {
		return dtmfSendPlan{}, err
	}
	if r.dtmfSeedErr != nil && !r.dtmfSourceObserved {
		return dtmfSendPlan{}, r.dtmfSeedErr
	}
	r.dtmfSending = true
	return dtmfSendPlan{
		remote: cloneUDPAddr(remote), event: event, steps: steps, finalDuration: finalDuration,
		payloadType: r.dtmfPayloadType, clockRate: r.dtmfClockRate,
	}, nil
}
