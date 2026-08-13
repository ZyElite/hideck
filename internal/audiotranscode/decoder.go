package audiotranscode

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (t *Transcoder) decodeInput(ctx context.Context, inputPath string) ([]int16, int, error) {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, 0, fmt.Errorf("read source recording: %w", err)
	}
	switch strings.ToLower(filepath.Ext(inputPath)) {
	case ".amr":
		decoder, err := t.recordingDecoder("AMR")
		if err != nil {
			return nil, 0, err
		}
		return decodeAMR(ctx, amrDecodeConfig{
			decoder: decoder, data: data, frameSizes: amrNBFrameBytes[:],
			magic: "#!AMR\n", samplesPerFrame: 160, sampleRate: 8000,
		})
	case ".amr-wb":
		decoder, err := t.recordingDecoder("AMR-WB")
		if err != nil {
			return nil, 0, err
		}
		return decodeAMR(ctx, amrDecodeConfig{
			decoder: decoder, data: data, frameSizes: amrWBFrameBytes[:],
			magic: "#!AMR-WB\n", samplesPerFrame: 320, sampleRate: 16000,
		})
	case ".wav":
		return decodePCM16WAV(data)
	default:
		return nil, 0, fmt.Errorf("unsupported recording format %q", filepath.Ext(inputPath))
	}
}

func (t *Transcoder) recordingDecoder(codec string) (*amrDecoderAPI, error) {
	if codec == "AMR" {
		t.amrNBDecoderOnce.Do(func() {
			t.amrNBDecoder, t.amrNBDecoderErr = loadRecordingAMRDecoder(codec)
		})
		return t.amrNBDecoder, t.amrNBDecoderErr
	}
	t.amrWBDecoderOnce.Do(func() {
		t.amrWBDecoder, t.amrWBDecoderErr = loadRecordingAMRDecoder(codec)
	})
	return t.amrWBDecoder, t.amrWBDecoderErr
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
