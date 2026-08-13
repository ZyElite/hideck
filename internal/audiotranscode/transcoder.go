package audiotranscode

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	mp3BitrateNB       = 32
	mp3BitrateWB       = 48
	mp3EncoderQuality  = 3
	mp3MonoMode        = 3
	mp3SamplesPerChunk = 1152
	mp3BufferPadding   = 7200
)

var (
	amrNBFrameBytes = [...]int{12, 13, 15, 17, 19, 20, 26, 31, 5, 0, 0, 0, 0, 0, 0, 0}
	amrWBFrameBytes = [...]int{17, 23, 32, 36, 40, 46, 50, 58, 60, 5, 0, 0, 0, 0, 0, 0}
)

type nativeLibraries struct {
	lame  *lameAPI
	amrNB *amrDecoderAPI
	amrWB *amrDecoderAPI
}

type amrDecoderAPI struct {
	init   func() uintptr
	decode func(uintptr, []byte, []int16, int)
	close  func(uintptr)
}

type lameAPI struct {
	init          func() uintptr
	close         func(uintptr) int
	setSampleRate func(uintptr, int) int
	setChannels   func(uintptr, int) int
	setBitrate    func(uintptr, int) int
	setMode       func(uintptr, int) int
	setQuality    func(uintptr, int) int
	initParams    func(uintptr) int
	encodeBuffer  func(uintptr, []int16, []int16, int, []byte, int) int
	encodeFlush   func(uintptr, []byte, int) int
}

// Transcoder converts the direct call recording through injected system codec libraries.
type Transcoder struct {
	once              sync.Once
	libs              nativeLibraries
	err               error
	amrNBRealtimeOnce sync.Once
	amrNBRealtime     *amrRealtimeAPI
	amrNBRealtimeErr  error
	amrWBRealtimeOnce sync.Once
	amrWBRealtime     *amrRealtimeAPI
	amrWBRealtimeErr  error
}

// New creates a lazy-loading system MP3 transcoder.
func New() *Transcoder { return &Transcoder{} }

// ToMP3 decodes the recorded AMR/WAV source and atomically publishes an MP3 file.
func (t *Transcoder) ToMP3(ctx context.Context, inputPath string) (string, error) {
	if t == nil {
		return "", errors.New("audio transcoder is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	t.once.Do(func() { t.libs, t.err = loadNativeLibraries() })
	if t.err != nil {
		return "", t.err
	}
	pcm, sampleRate, err := t.decodeInput(ctx, strings.TrimSpace(inputPath))
	if err != nil {
		return "", err
	}
	outputPath := replaceAudioExtension(inputPath, ".mp3")
	if err := encodeMP3File(ctx, mp3EncodeRequest{
		api: t.libs.lame, outputPath: outputPath, pcm: pcm, sampleRate: sampleRate,
	}); err != nil {
		return "", err
	}
	return outputPath, nil
}

func (t *Transcoder) decodeInput(ctx context.Context, inputPath string) ([]int16, int, error) {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, 0, fmt.Errorf("read source recording: %w", err)
	}
	switch strings.ToLower(filepath.Ext(inputPath)) {
	case ".amr":
		return decodeAMR(ctx, amrDecodeConfig{
			decoder: t.libs.amrNB, data: data, frameSizes: amrNBFrameBytes[:],
			magic: "#!AMR\n", samplesPerFrame: 160, sampleRate: 8000,
		})
	case ".amr-wb":
		return decodeAMR(ctx, amrDecodeConfig{
			decoder: t.libs.amrWB, data: data, frameSizes: amrWBFrameBytes[:],
			magic: "#!AMR-WB\n", samplesPerFrame: 320, sampleRate: 16000,
		})
	case ".wav":
		return decodePCM16WAV(data)
	default:
		return nil, 0, fmt.Errorf("unsupported recording format %q", filepath.Ext(inputPath))
	}
}

func replaceAudioExtension(path, extension string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + extension
}

type amrDecodeConfig struct {
	decoder         *amrDecoderAPI
	data            []byte
	frameSizes      []int
	magic           string
	samplesPerFrame int
	sampleRate      int
}

func decodeAMR(ctx context.Context, config amrDecodeConfig) ([]int16, int, error) {
	if config.decoder == nil {
		return nil, 0, errors.New("AMR decoder is unavailable")
	}
	if !strings.HasPrefix(string(config.data), config.magic) {
		return nil, 0, errors.New("invalid AMR recording header")
	}
	state := config.decoder.init()
	if state == 0 {
		return nil, 0, errors.New("initialize AMR decoder")
	}
	defer config.decoder.close(state)
	offset := len(config.magic)
	pcm := make([]int16, 0, len(config.data)*4)
	for offset < len(config.data) {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		frameType := int((config.data[offset] >> 3) & 0x0f)
		if config.frameSizes[frameType] == 0 && frameType < 14 {
			return nil, 0, fmt.Errorf("unsupported AMR frame type %d", frameType)
		}
		frameLength := 1 + config.frameSizes[frameType]
		if offset+frameLength > len(config.data) {
			return nil, 0, errors.New("truncated AMR recording frame")
		}
		framePCM := make([]int16, config.samplesPerFrame)
		badFrame := 0
		if config.data[offset]&0x04 == 0 {
			badFrame = 1
		}
		config.decoder.decode(state, config.data[offset:offset+frameLength], framePCM, badFrame)
		pcm = append(pcm, framePCM...)
		offset += frameLength
	}
	if len(pcm) == 0 {
		return nil, 0, errors.New("AMR recording contains no audio frames")
	}
	return pcm, config.sampleRate, nil
}

func decodePCM16WAV(data []byte) ([]int16, int, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, errors.New("invalid WAV recording header")
	}
	var sampleRate int
	var pcmBytes []byte
	for offset := 12; offset+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		start, end := offset+8, offset+8+size
		if end > len(data) {
			return nil, 0, errors.New("truncated WAV chunk")
		}
		switch string(data[offset : offset+4]) {
		case "fmt ":
			if size < 16 || binary.LittleEndian.Uint16(data[start:start+2]) != 1 ||
				binary.LittleEndian.Uint16(data[start+2:start+4]) != 1 ||
				binary.LittleEndian.Uint16(data[start+14:start+16]) != 16 {
				return nil, 0, errors.New("WAV recording must be mono PCM16")
			}
			sampleRate = int(binary.LittleEndian.Uint32(data[start+4 : start+8]))
		case "data":
			pcmBytes = data[start:end]
		}
		offset = end + size%2
	}
	if sampleRate <= 0 || len(pcmBytes) == 0 || len(pcmBytes)%2 != 0 {
		return nil, 0, errors.New("WAV recording has no valid PCM data")
	}
	pcm := make([]int16, len(pcmBytes)/2)
	for index := range pcm {
		pcm[index] = int16(binary.LittleEndian.Uint16(pcmBytes[index*2:]))
	}
	return pcm, sampleRate, nil
}

type mp3EncodeRequest struct {
	api        *lameAPI
	outputPath string
	pcm        []int16
	sampleRate int
}

func encodeMP3File(ctx context.Context, request mp3EncodeRequest) (resultErr error) {
	if request.api == nil {
		return errors.New("MP3 encoder is unavailable")
	}
	encoder := request.api.init()
	if encoder == 0 {
		return errors.New("initialize MP3 encoder")
	}
	defer request.api.close(encoder)
	bitrate := mp3BitrateNB
	if request.sampleRate > 8000 {
		bitrate = mp3BitrateWB
	}
	if err := request.api.configure(encoder, request.sampleRate, bitrate); err != nil {
		return err
	}
	temporaryPath := request.outputPath + ".part"
	file, err := os.OpenFile(temporaryPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create MP3 recording: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			resultErr = errors.Join(resultErr, file.Close())
		}
		if resultErr != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	buffer := make([]byte, mp3SamplesPerChunk*5/4+mp3BufferPadding)
	for offset := 0; offset < len(request.pcm); offset += mp3SamplesPerChunk {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(offset+mp3SamplesPerChunk, len(request.pcm))
		written := request.api.encode(encoder, request.pcm[offset:end], buffer)
		if err := writeEncodedMP3(file, buffer, written); err != nil {
			return err
		}
	}
	written := request.api.flush(encoder, buffer)
	if err := writeEncodedMP3(file, buffer, written); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close MP3 recording: %w", err)
	}
	closed = true
	if err := os.Rename(temporaryPath, request.outputPath); err != nil {
		return fmt.Errorf("publish MP3 recording: %w", err)
	}
	return nil
}

func writeEncodedMP3(file *os.File, buffer []byte, written int) error {
	if written < 0 {
		return fmt.Errorf("MP3 encoder failed with code %d", written)
	}
	if written == 0 {
		return nil
	}
	if _, err := file.Write(buffer[:written]); err != nil {
		return fmt.Errorf("write MP3 recording: %w", err)
	}
	return nil
}

func (api *lameAPI) configure(encoder uintptr, sampleRate, bitrate int) error {
	settings := []struct {
		name   string
		result int
	}{
		{"sample rate", api.setSampleRate(encoder, sampleRate)},
		{"channel count", api.setChannels(encoder, 1)},
		{"mono mode", api.setMode(encoder, mp3MonoMode)},
		{"bitrate", api.setBitrate(encoder, bitrate)},
		{"quality", api.setQuality(encoder, mp3EncoderQuality)},
		{"parameters", api.initParams(encoder)},
	}
	for _, setting := range settings {
		if setting.result < 0 {
			return fmt.Errorf("configure MP3 %s: code %d", setting.name, setting.result)
		}
	}
	return nil
}

func (api *lameAPI) encode(encoder uintptr, pcm []int16, output []byte) int {
	return api.encodeBuffer(encoder, pcm, pcm, len(pcm), output, len(output))
}

func (api *lameAPI) flush(encoder uintptr, output []byte) int {
	return api.encodeFlush(encoder, output, len(output))
}
