package phone

import "testing"

func TestG711RoundTripsRepresentativePCM(t *testing.T) {
	samples := []int16{-30000, -10000, -1000, -1, 0, 1, 1000, 10000, 30000}
	for _, sample := range samples {
		muRoundTrip := muLawToPCM(pcmToMuLaw(sample))
		if absolutePCMDelta(sample, muRoundTrip) > 1024 {
			t.Fatalf("PCMU round trip %d -> %d", sample, muRoundTrip)
		}
		aRoundTrip := aLawToPCM(pcmToALaw(sample))
		if absolutePCMDelta(sample, aRoundTrip) > 1024 {
			t.Fatalf("PCMA round trip %d -> %d", sample, aRoundTrip)
		}
	}
}

func TestTranscodeG711ConvertsInBothDirections(t *testing.T) {
	muLaw := []byte{0xff, 0x7f, 0x00, 0x80}
	aLaw := append([]byte(nil), muLaw...)
	transcodeG711(aLaw, "PCMU", "PCMA")
	for index, encoded := range aLaw {
		want := pcmToALaw(muLawToPCM(muLaw[index]))
		if encoded != want {
			t.Fatalf("PCMU->PCMA byte %d = %#x, want %#x", index, encoded, want)
		}
	}
	transcodeG711(aLaw, "PCMA", "PCMU")
	for index, encoded := range aLaw {
		want := pcmToMuLaw(aLawToPCM(pcmToALaw(muLawToPCM(muLaw[index]))))
		if encoded != want {
			t.Fatalf("PCMA->PCMU byte %d = %#x, want %#x", index, encoded, want)
		}
	}
}

func absolutePCMDelta(left, right int16) int {
	delta := int(left) - int(right)
	if delta < 0 {
		return -delta
	}
	return delta
}
