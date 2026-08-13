package phone

import (
	"strings"
	"testing"
)

func TestParseRTPEndpointSelectsNegotiatedG711(t *testing.T) {
	tests := []struct {
		name, mapping, codec string
		payload              uint8
	}{
		{name: "static PCMU", mapping: "m=audio 41000 RTP/AVP 0\r\n", codec: "PCMU", payload: 0},
		{name: "dynamic PCMA", mapping: "m=audio 41000 RTP/AVP 112\r\na=rtpmap:112 PCMA/8000\r\n", codec: "PCMA", payload: 112},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			endpoint, err := parseRTPEndpoint("v=0\r\nc=IN IP4 127.0.0.1\r\n" + test.mapping)
			if err != nil {
				t.Fatal(err)
			}
			if endpoint.Codec != test.codec || endpoint.PayloadType != test.payload || endpoint.Address.Port != 41000 {
				t.Fatalf("endpoint = %+v", endpoint)
			}
		})
	}
}

func TestParseRTPEndpointPreservesAMRParameters(t *testing.T) {
	endpoint, err := parseRTPEndpoint("v=0\r\nc=IN IP4 127.0.0.1\r\nm=audio 41000 RTP/AVP 104\r\na=rtpmap:104 AMR-WB/16000\r\na=fmtp:104 octet-align=1; mode-set=0,2\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.Codec != "AMR-WB" || endpoint.ClockRate != 16000 || endpoint.Fmtp != "octet-align=1; mode-set=0,2" {
		t.Fatalf("endpoint = %+v", endpoint)
	}
}

func TestParseRTPEndpointUsesG711WhenAMREncoderIsUnavailable(t *testing.T) {
	raw := "v=0\r\nc=IN IP4 127.0.0.1\r\nm=audio 41000 RTP/AVP 104 0\r\na=rtpmap:104 AMR-WB/16000\r\na=fmtp:104 octet-align=1\r\n"
	endpoint, err := parseRTPEndpoint(raw, "PCMU", "PCMA")
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.Codec != "PCMU" {
		t.Fatalf("endpoint = %+v", endpoint)
	}
	_, err = parseRTPEndpoint(strings.Replace(raw, "104 0", "104", 1), "PCMU", "PCMA")
	if err == nil || !strings.Contains(err.Error(), "AMR-WB") || !strings.Contains(err.Error(), "unavailable encoder") {
		t.Fatalf("AMR-only error = %v", err)
	}
}

func TestPlainAudioSDPAdvertisesOnlySupportedIMSCodecsAndDTMF(t *testing.T) {
	sdp := plainAudioSDP(42000, nil)
	for _, expected := range []string{"m=audio 42000 RTP/AVP 0 8 101", "PCMU/8000", "PCMA/8000", "telephone-event/8000", "a=ptime:20"} {
		if !strings.Contains(sdp, expected) {
			t.Fatalf("SDP missing %q: %s", expected, sdp)
		}
	}
	withAMR := plainAudioSDP(42000, []string{"AMR-WB", "AMR"})
	for _, expected := range []string{"RTP/AVP 104 114 0 8 101", "AMR-WB/16000", "AMR/8000", "octet-align=1"} {
		if !strings.Contains(withAMR, expected) {
			t.Fatalf("AMR SDP missing %q: %s", expected, withAMR)
		}
	}
	answer := plainSelectedAudioSDP(42000, rtpEndpoint{
		Codec: "PCMU", PayloadType: 0, ClockRate: 8000,
	})
	if !strings.Contains(answer, "RTP/AVP 0 101") || strings.Contains(answer, "PCMA") {
		t.Fatalf("selected answer SDP = %s", answer)
	}
}
