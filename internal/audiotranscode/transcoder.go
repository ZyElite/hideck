package audiotranscode

import (
	"context"
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
	lameOnce          sync.Once
	lame              *lameAPI
	lameErr           error
	amrNBDecoderOnce  sync.Once
	amrNBDecoder      *amrDecoderAPI
	amrNBDecoderErr   error
	amrWBDecoderOnce  sync.Once
	amrWBDecoder      *amrDecoderAPI
	amrWBDecoderErr   error
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
	t.lameOnce.Do(func() { t.lame, t.lameErr = loadNativeLame() })
	if t.lameErr != nil {
		return "", t.lameErr
	}
	pcm, sampleRate, err := t.decodeInput(ctx, strings.TrimSpace(inputPath))
	if err != nil {
		return "", err
	}
	outputPath := replaceAudioExtension(inputPath, ".mp3")
	if err := encodeMP3File(ctx, mp3EncodeRequest{
		api: t.lame, outputPath: outputPath, pcm: pcm, sampleRate: sampleRate,
	}); err != nil {
		return "", err
	}
	return outputPath, nil
}

func replaceAudioExtension(path, extension string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + extension
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
