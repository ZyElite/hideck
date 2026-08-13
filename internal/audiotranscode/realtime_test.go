package audiotranscode

import (
	"strings"
	"testing"
)

func TestRealtimeCodecFramesAMRInBothDirections(t *testing.T) {
	for _, test := range []struct {
		name, fmtp      string
		sampleRate      int
		samplesPerFrame int
		frameSizes      []int
		maxMode         int
	}{
		{name: "AMR", fmtp: "octet-align=1; mode-set=0,2,7", sampleRate: 8000, samplesPerFrame: 160, frameSizes: amrNBFrameBytes[:], maxMode: 7},
		{name: "AMR-WB", fmtp: "octet-align=1; mode-set=0,2", sampleRate: 16000, samplesPerFrame: 320, frameSizes: amrWBFrameBytes[:], maxMode: 8},
	} {
		t.Run(test.name, func(t *testing.T) {
			capture := &fakeRealtimeNative{}
			api := capture.api(test.frameSizes)
			config := realtimeCodecConfig{
				name: test.name, sampleRate: test.sampleRate, samplesPerFrame: test.samplesPerFrame,
				frameSizes: test.frameSizes, maxMode: test.maxMode, api: api,
			}
			mode, err := selectAMRMode(test.fmtp, test.maxMode)
			if err != nil {
				t.Fatal(err)
			}
			codec, err := newRealtimeCodec(config, mode)
			if err != nil {
				t.Fatal(err)
			}
			defer codec.Close()
			payload := amrTestPayload(test.frameSizes, mode)
			pcm, err := codec.Decode(payload)
			if err != nil {
				t.Fatal(err)
			}
			if len(pcm) != test.samplesPerFrame || capture.decodedFrame[0] != byte(mode<<3)|amrQualityBit {
				t.Fatalf("decoded samples=%d frame header=%#x", len(pcm), capture.decodedFrame[0])
			}
			encoded, err := codec.Encode(pcm)
			if err != nil {
				t.Fatal(err)
			}
			if capture.encodedMode != mode || len(encoded) != 2+test.frameSizes[mode] || encoded[0] != amrNoModeRequest {
				t.Fatalf("mode=%d payload=%x", capture.encodedMode, encoded)
			}
		})
	}
}

func TestRealtimeCodecRejectsUnsupportedPacketization(t *testing.T) {
	if _, err := selectAMRMode("mode-set=0,1", 7); err == nil || !strings.Contains(err.Error(), "octet-align") {
		t.Fatalf("missing octet-align error = %v", err)
	}
	payload := amrTestPayload(amrNBFrameBytes[:], 7)
	payload[1] |= 0x80
	if _, _, err := parseOctetAlignedFrame(payload, amrNBFrameBytes[:]); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("multiple frame error = %v", err)
	}
}

type fakeRealtimeNative struct {
	decodedFrame []byte
	encodedMode  int
}

func (fake *fakeRealtimeNative) api(frameSizes []int) *amrRealtimeAPI {
	return &amrRealtimeAPI{
		decoder: &amrDecoderAPI{
			init: func() uintptr { return 1 },
			decode: func(_ uintptr, frame []byte, pcm []int16, _ int) {
				fake.decodedFrame = append([]byte(nil), frame...)
				for index := range pcm {
					pcm[index] = int16(index)
				}
			},
			close: func(uintptr) {},
		},
		encoder: &amrEncoderAPI{
			init: func() uintptr { return 2 },
			encode: func(_ uintptr, mode int, _ []int16, output []byte) int {
				fake.encodedMode = mode
				length := frameSizes[mode] + 1
				output[0] = byte(mode<<3) | amrQualityBit
				for index := 1; index < length; index++ {
					output[index] = byte(index)
				}
				return length
			},
			close: func(uintptr) {},
		},
	}
}

func amrTestPayload(frameSizes []int, mode int) []byte {
	payload := make([]byte, 2+frameSizes[mode])
	payload[0], payload[1] = amrNoModeRequest, byte(mode<<3)|amrQualityBit
	for index := 2; index < len(payload); index++ {
		payload[index] = byte(index)
	}
	return payload
}
