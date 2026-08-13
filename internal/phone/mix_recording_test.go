package phone

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestMixedRecorderWritesBothDirectionsToPCM16WAV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "call_mix.wav")
	recorder, err := newMixedRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	toIMS := repeatByte(pcmToMuLaw(2000), browserSamplesPerFrame)
	fromIMS := repeatByte(pcmToMuLaw(-1000), browserSamplesPerFrame)
	recorder.Add(mixToIMS, toIMS)
	recorder.Add(mixFromIMS, fromIMS)
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" ||
		binary.LittleEndian.Uint32(data[40:44]) != browserSamplesPerFrame*2 {
		t.Fatalf("invalid WAV header: %x", data[:mixedWAVHeaderBytes])
	}
	got := int16(binary.LittleEndian.Uint16(data[mixedWAVHeaderBytes:]))
	want := int16((int(muLawToPCM(toIMS[0])) + int(muLawToPCM(fromIMS[0]))) / 2)
	if got != want {
		t.Fatalf("mixed sample=%d, want %d", got, want)
	}
	if err := recorder.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestMixedRecorderReportsEmptyAudio(t *testing.T) {
	recorder, err := newMixedRecorder(filepath.Join(t.TempDir(), "empty.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err == nil {
		t.Fatal("empty mixed recording did not report an error")
	}
}
