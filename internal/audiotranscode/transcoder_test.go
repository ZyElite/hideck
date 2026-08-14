package audiotranscode

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodePCM16WAV(t *testing.T) {
	data := testPCM16WAV()

	pcm, sampleRate, err := decodePCM16WAV(data)
	if err != nil {
		t.Fatal(err)
	}
	if sampleRate != 8000 || len(pcm) != 2 || pcm[0] != 0x1234 || pcm[1] != -0x1234 {
		t.Fatalf("sampleRate=%d pcm=%v", sampleRate, pcm)
	}
}

func testPCM16WAV() []byte {
	header := make([]byte, 44)
	copy(header[:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], 40)
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], 8000)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], 4)
	return append(header, 0x34, 0x12, 0xcc, 0xed)
}

func TestWAVToMP3DoesNotRequireAMRDecoders(t *testing.T) {
	input := filepath.Join(t.TempDir(), "call_test.wav")
	if err := os.WriteFile(input, testPCM16WAV(), 0o644); err != nil {
		t.Fatal(err)
	}
	transcoder := New()
	transcoder.lame = fakeLameAPI()
	transcoder.lameOnce.Do(func() {})
	transcoder.amrNBDecoderErr = context.Canceled
	transcoder.amrWBDecoderErr = context.Canceled
	output, err := transcoder.ToMP3(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "x" {
		t.Fatalf("encoded MP3 bytes=%q", data)
	}
}

func fakeLameAPI() *lameAPI {
	return &lameAPI{
		init: func() uintptr { return 1 }, close: func(uintptr) int { return 0 },
		setSampleRate: func(uintptr, int) int { return 0 },
		setChannels:   func(uintptr, int) int { return 0 },
		setBitrate:    func(uintptr, int) int { return 0 },
		setMode:       func(uintptr, int) int { return 0 },
		setQuality:    func(uintptr, int) int { return 0 },
		initParams:    func(uintptr) int { return 0 },
		encodeBuffer: func(_ uintptr, _, _ []int16, _ int, output []byte, _ int) int {
			output[0] = 'x'
			return 1
		},
		encodeFlush: func(uintptr, []byte, int) int { return 0 },
	}
}

func TestDecodePCM16WAVRejectsUnsupportedInput(t *testing.T) {
	_, _, err := decodePCM16WAV([]byte("not a wave file"))
	if err == nil || !strings.Contains(err.Error(), "header") {
		t.Fatalf("error=%v", err)
	}
}

func TestReplaceAudioExtension(t *testing.T) {
	if got := replaceAudioExtension("call.amr-wb", ".mp3"); got != "call.mp3" {
		t.Fatalf("path=%q", got)
	}
}

func TestNativeTranscodeWhenConfigured(t *testing.T) {
	inputPath := strings.TrimSpace(os.Getenv("HIDECK_AUDIO_TRANSCODE_TEST_INPUT"))
	if inputPath == "" {
		t.Skip("HIDECK_AUDIO_TRANSCODE_TEST_INPUT is not set")
	}
	outputPath, err := New().ToMP3(context.Background(), inputPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(outputPath) != ".mp3" || info.Size() == 0 {
		t.Fatalf("output=%q size=%d", outputPath, info.Size())
	}
}
