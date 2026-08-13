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
	data := append(header, 0x34, 0x12, 0xcc, 0xed)

	pcm, sampleRate, err := decodePCM16WAV(data)
	if err != nil {
		t.Fatal(err)
	}
	if sampleRate != 8000 || len(pcm) != 2 || pcm[0] != 0x1234 || pcm[1] != -0x1234 {
		t.Fatalf("sampleRate=%d pcm=%v", sampleRate, pcm)
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
	inputPath := strings.TrimSpace(os.Getenv("VOHIVE_AUDIO_TRANSCODE_TEST_INPUT"))
	if inputPath == "" {
		t.Skip("VOHIVE_AUDIO_TRANSCODE_TEST_INPUT is not set")
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
