package phone

import (
	"strings"
	"testing"
)

func TestAMRMediaPayloadConversionAndClockScaling(t *testing.T) {
	for _, test := range []struct {
		name       string
		sampleRate int
		pcmSamples int
	}{
		{name: "AMR", sampleRate: 8000, pcmSamples: 160},
		{name: "AMR-WB", sampleRate: 16000, pcmSamples: 320},
	} {
		t.Run(test.name, func(t *testing.T) {
			codec := &vectorRealtimeCodec{sampleRate: test.sampleRate, pcmSamples: test.pcmSamples}
			endpoint := rtpEndpoint{Codec: test.name, ClockRate: test.sampleRate}
			browser := make([]byte, 160)
			for index := range browser {
				browser[index] = pcmToMuLaw(int16(index * 10))
			}
			toIMS, err := browserPayloadForIMS(browser, endpoint, codec)
			if err != nil {
				t.Fatal(err)
			}
			if len(codec.encodedPCM) != test.pcmSamples || string(toIMS) != "encoded" {
				t.Fatalf("encoded samples=%d payload=%q", len(codec.encodedPCM), toIMS)
			}
			fromIMS, err := imsPayloadForBrowser([]byte("frame"), endpoint, codec)
			if err != nil {
				t.Fatal(err)
			}
			if len(fromIMS) != 160 {
				t.Fatalf("browser samples=%d, want 160", len(fromIMS))
			}
			if got := scaleTimestamp(160, browserClockRate, test.sampleRate); got != uint32(test.sampleRate/50) {
				t.Fatalf("outbound timestamp=%d", got)
			}
			if got := scaleTimestamp(uint32(test.sampleRate/50), test.sampleRate, browserClockRate); got != 160 {
				t.Fatalf("inbound timestamp=%d", got)
			}
		})
	}
}

func TestAMRAttachRejectsMissingEncoderExplicitly(t *testing.T) {
	session := &MediaSession{}
	_, err := session.createRealtimeCodec(rtpEndpoint{Codec: "AMR-WB", ClockRate: 16000})
	if err == nil || !strings.Contains(err.Error(), "AMR-WB") || !strings.Contains(err.Error(), "unavailable encoder") {
		t.Fatalf("error = %v", err)
	}
}

type vectorRealtimeCodec struct {
	sampleRate int
	pcmSamples int
	encodedPCM []int16
}

func (codec *vectorRealtimeCodec) SampleRate() int { return codec.sampleRate }

func (codec *vectorRealtimeCodec) Decode([]byte) ([]int16, error) {
	pcm := make([]int16, codec.pcmSamples)
	for index := range pcm {
		pcm[index] = int16(index * 10)
	}
	return pcm, nil
}

func (codec *vectorRealtimeCodec) Encode(pcm []int16) ([]byte, error) {
	codec.encodedPCM = append([]int16(nil), pcm...)
	return []byte("encoded"), nil
}

func (codec *vectorRealtimeCodec) Close() error { return nil }
