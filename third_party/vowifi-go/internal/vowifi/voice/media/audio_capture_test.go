package media

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestRTPAudioRecorderWritesAMRWB(t *testing.T) {
	base := filepath.Join(t.TempDir(), "call")
	recorder, err := newRTPAudioRecorder(base, []AudioCodec{{
		PayloadType: 104, Name: "AMR-WB", ClockRate: 16000, Fmtp: "octet-align=1; max-red=0",
	}})
	if err != nil {
		t.Fatal(err)
	}
	payload := append([]byte{0xf0, 0x04}, make([]byte, amrWBFrameBytes[0])...)
	if err := recorder.writeRTP(testRTPPacket(104, payload)); err != nil {
		t.Fatal(err)
	}
	if err := recorder.close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(base + ".amr-wb")
	if err != nil {
		t.Fatal(err)
	}
	if string(data[:9]) != "#!AMR-WB\n" || data[9] != 0x04 || len(data) != 27 {
		t.Fatalf("AMR-WB recording is malformed: length=%d prefix=%q toc=%#x", len(data), data[:9], data[9])
	}
}

func TestRTPAudioRecorderWritesPCMWave(t *testing.T) {
	base := filepath.Join(t.TempDir(), "call")
	recorder, err := newRTPAudioRecorder(base, []AudioCodec{{
		PayloadType: 0, Name: "PCMU", ClockRate: 8000,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.writeRTP(testRTPPacket(0, make([]byte, 160))); err != nil {
		t.Fatal(err)
	}
	if err := recorder.close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(base + ".wav")
	if err != nil {
		t.Fatal(err)
	}
	if string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		t.Fatalf("WAV header is malformed: %q", data[:12])
	}
	if got := binary.LittleEndian.Uint32(data[40:44]); got != 320 || len(data) != wavHeaderLength+320 {
		t.Fatalf("WAV data length=%d file length=%d", got, len(data))
	}
}

func TestRTPAudioRecorderRejectsEmptyRecording(t *testing.T) {
	recorder, err := newRTPAudioRecorder(filepath.Join(t.TempDir(), "call"), []AudioCodec{{
		PayloadType: 0, Name: "PCMU", ClockRate: 8000,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.close(); err == nil || err.Error() != "media: recording contains no audio frames" {
		t.Fatalf("close error=%v", err)
	}
}

func testRTPPacket(payloadType int, payload []byte) []byte {
	packet := make([]byte, rtpFixedHeaderLength+len(payload))
	packet[0] = 0x80
	packet[1] = byte(payloadType)
	copy(packet[rtpFixedHeaderLength:], payload)
	return packet
}
