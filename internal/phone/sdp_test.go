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

func TestParseRTPEndpointRejectsUnavailableAMRExplicitly(t *testing.T) {
	_, err := parseRTPEndpoint("v=0\r\nc=IN IP4 127.0.0.1\r\nm=audio 41000 RTP/AVP 104\r\na=rtpmap:104 AMR-WB/16000\r\n")
	if err == nil || !strings.Contains(err.Error(), "AMR-WB") || !strings.Contains(err.Error(), "unavailable encoder") {
		t.Fatalf("error = %v", err)
	}
}

func TestPlainAudioSDPAdvertisesOnlySupportedIMSCodecsAndDTMF(t *testing.T) {
	sdp := plainAudioSDP(42000)
	for _, expected := range []string{"m=audio 42000 RTP/AVP 0 8 101", "PCMU/8000", "PCMA/8000", "telephone-event/8000", "a=ptime:20"} {
		if !strings.Contains(sdp, expected) {
			t.Fatalf("SDP missing %q: %s", expected, sdp)
		}
	}
}
