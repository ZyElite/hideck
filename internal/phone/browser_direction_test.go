package phone

import "testing"

func TestBrowserOfferReceivesOnlyAudio(t *testing.T) {
	tests := []struct {
		name string
		sdp  string
		want bool
	}{
		{name: "media receive only", sdp: directionSDP("a=recvonly\r\n"), want: true},
		{name: "media send receive", sdp: directionSDP("a=sendrecv\r\n")},
		{name: "default send receive", sdp: directionSDP("")},
		{name: "media receive only overrides session", sdp: directionSDP("a=sendrecv\r\n", "a=recvonly\r\n"), want: true},
		{name: "media send receive overrides session", sdp: directionSDP("a=recvonly\r\n", "a=sendrecv\r\n")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := browserOfferReceivesOnlyAudio(test.sdp)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("receive-only = %t, want %t", got, test.want)
			}
		})
	}
}

func directionSDP(attributes ...string) string {
	session, media := "", ""
	if len(attributes) == 1 {
		media = attributes[0]
	} else if len(attributes) == 2 {
		session, media = attributes[0], attributes[1]
	}
	return "v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n" + session +
		"m=audio 9 UDP/TLS/RTP/SAVPF 0\r\n" + media
}
