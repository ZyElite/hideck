package audiotranscode

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

const (
	amrNoModeRequest        = 0xf0
	amrQualityBit           = 0x04
	maxAMRStorageFrameBytes = 64
)

type amrEncoderAPI struct {
	init   func() uintptr
	encode func(uintptr, int, []int16, []byte) int
	close  func(uintptr)
}

type amrRealtimeAPI struct {
	decoder *amrDecoderAPI
	encoder *amrEncoderAPI
}

type realtimeCodecConfig struct {
	name            string
	sampleRate      int
	samplesPerFrame int
	frameSizes      []int
	maxMode         int
	api             *amrRealtimeAPI
}

// RealtimeCodec owns one native decoder and encoder pair for a call leg.
type RealtimeCodec struct {
	config       realtimeCodecConfig
	mode         int
	decoderState uintptr
	encoderState uintptr
	mu           sync.RWMutex
	closed       bool
}

func (t *Transcoder) ValidateRealtimeCodec(codec string) error {
	_, err := t.realtimeAPI(codec)
	return err
}

func (t *Transcoder) NewRealtimeCodec(codec, fmtp string) (*RealtimeCodec, error) {
	name := strings.ToUpper(strings.TrimSpace(codec))
	api, err := t.realtimeAPI(name)
	if err != nil {
		return nil, err
	}
	config, err := realtimeConfig(name, api)
	if err != nil {
		return nil, err
	}
	mode, err := selectAMRMode(fmtp, config.maxMode)
	if err != nil {
		return nil, fmt.Errorf("%s realtime codec: %w", name, err)
	}
	return newRealtimeCodec(config, mode)
}

func (t *Transcoder) realtimeAPI(codec string) (*amrRealtimeAPI, error) {
	if t == nil {
		return nil, errors.New("native realtime audio transcoder is unavailable")
	}
	switch strings.ToUpper(strings.TrimSpace(codec)) {
	case "AMR":
		t.amrNBRealtimeOnce.Do(func() { t.amrNBRealtime, t.amrNBRealtimeErr = loadAMRNBRealtimeAPI() })
		return t.amrNBRealtime, t.amrNBRealtimeErr
	case "AMR-WB":
		t.amrWBRealtimeOnce.Do(func() { t.amrWBRealtime, t.amrWBRealtimeErr = loadAMRWBRealtimeAPI() })
		return t.amrWBRealtime, t.amrWBRealtimeErr
	default:
		return nil, fmt.Errorf("unsupported realtime codec %q", codec)
	}
}

func realtimeConfig(name string, api *amrRealtimeAPI) (realtimeCodecConfig, error) {
	switch name {
	case "AMR":
		return realtimeCodecConfig{
			name: name, sampleRate: 8000, samplesPerFrame: 160,
			frameSizes: amrNBFrameBytes[:], maxMode: 7, api: api,
		}, nil
	case "AMR-WB":
		return realtimeCodecConfig{
			name: name, sampleRate: 16000, samplesPerFrame: 320,
			frameSizes: amrWBFrameBytes[:], maxMode: 8, api: api,
		}, nil
	default:
		return realtimeCodecConfig{}, fmt.Errorf("unsupported realtime codec %q", name)
	}
}

func newRealtimeCodec(config realtimeCodecConfig, mode int) (*RealtimeCodec, error) {
	if config.api == nil || config.api.decoder == nil || config.api.encoder == nil {
		return nil, fmt.Errorf("%s native decoder and encoder are required", config.name)
	}
	codec := &RealtimeCodec{config: config, mode: mode}
	codec.decoderState = config.api.decoder.init()
	if codec.decoderState == 0 {
		return nil, fmt.Errorf("initialize %s decoder", config.name)
	}
	codec.encoderState = config.api.encoder.init()
	if codec.encoderState == 0 {
		config.api.decoder.close(codec.decoderState)
		return nil, fmt.Errorf("initialize %s encoder", config.name)
	}
	return codec, nil
}

func (codec *RealtimeCodec) SampleRate() int { return codec.config.sampleRate }

func (codec *RealtimeCodec) Decode(payload []byte) ([]int16, error) {
	codec.mu.RLock()
	defer codec.mu.RUnlock()
	if codec.closed {
		return nil, errors.New("realtime codec is closed")
	}
	frame, badFrame, err := parseOctetAlignedFrame(payload, codec.config.frameSizes)
	if err != nil {
		return nil, err
	}
	pcm := make([]int16, codec.config.samplesPerFrame)
	codec.config.api.decoder.decode(codec.decoderState, frame, pcm, badFrame)
	return pcm, nil
}

func (codec *RealtimeCodec) Encode(pcm []int16) ([]byte, error) {
	codec.mu.RLock()
	defer codec.mu.RUnlock()
	if codec.closed {
		return nil, errors.New("realtime codec is closed")
	}
	if len(pcm) != codec.config.samplesPerFrame {
		return nil, fmt.Errorf("%s encoder needs %d samples, got %d", codec.config.name, codec.config.samplesPerFrame, len(pcm))
	}
	storage := make([]byte, maxAMRStorageFrameBytes)
	written := codec.config.api.encoder.encode(codec.encoderState, codec.mode, pcm, storage)
	return storageFrameToRTP(storage, written, codec.config.frameSizes)
}

func (codec *RealtimeCodec) Close() error {
	codec.mu.Lock()
	defer codec.mu.Unlock()
	if codec.closed {
		return nil
	}
	codec.closed = true
	codec.config.api.decoder.close(codec.decoderState)
	codec.config.api.encoder.close(codec.encoderState)
	return nil
}

func selectAMRMode(fmtp string, maxMode int) (int, error) {
	fields := parseFMTP(fmtp)
	if fields["octet-align"] != "1" {
		return 0, errors.New("octet-align=1 is required")
	}
	modeSet := strings.TrimSpace(fields["mode-set"])
	if modeSet == "" {
		return maxMode, nil
	}
	selected := -1
	for _, raw := range strings.Split(modeSet, ",") {
		mode, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || mode < 0 || mode > maxMode {
			return 0, fmt.Errorf("invalid mode-set %q", modeSet)
		}
		if mode > selected {
			selected = mode
		}
	}
	if selected < 0 {
		return 0, errors.New("mode-set is empty")
	}
	return selected, nil
}

func parseFMTP(value string) map[string]string {
	result := make(map[string]string)
	for _, field := range strings.Split(strings.ToLower(value), ";") {
		parts := strings.SplitN(strings.TrimSpace(field), "=", 2)
		if len(parts) == 2 {
			result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return result
}

func parseOctetAlignedFrame(payload []byte, frameSizes []int) ([]byte, int, error) {
	if len(payload) < 2 {
		return nil, 0, errors.New("truncated AMR RTP payload")
	}
	toc := payload[1]
	if toc&0x80 != 0 {
		return nil, 0, errors.New("multiple AMR frames per RTP packet are unsupported")
	}
	frameType := int((toc >> 3) & 0x0f)
	frameBytes := frameSizes[frameType]
	if frameBytes == 0 && frameType < 14 {
		return nil, 0, fmt.Errorf("unsupported AMR frame type %d", frameType)
	}
	if len(payload) != 2+frameBytes {
		return nil, 0, fmt.Errorf("AMR frame size is %d, want %d", len(payload), 2+frameBytes)
	}
	frame := make([]byte, 1+frameBytes)
	frame[0] = toc & 0x7c
	copy(frame[1:], payload[2:])
	badFrame := 0
	if toc&amrQualityBit == 0 {
		badFrame = 1
	}
	return frame, badFrame, nil
}

func storageFrameToRTP(storage []byte, written int, frameSizes []int) ([]byte, error) {
	if written <= 0 || written > len(storage) {
		return nil, fmt.Errorf("AMR encoder returned invalid length %d", written)
	}
	frameType := int((storage[0] >> 3) & 0x0f)
	frameBytes := frameSizes[frameType]
	if frameBytes == 0 || written != frameBytes+1 {
		return nil, fmt.Errorf("AMR encoder frame type %d has invalid length %d", frameType, written)
	}
	payload := make([]byte, frameBytes+2)
	payload[0], payload[1] = amrNoModeRequest, storage[0]&0x7c
	copy(payload[2:], storage[1:written])
	return payload, nil
}
