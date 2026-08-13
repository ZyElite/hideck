package phone

import "fmt"

func decodePCMU(payload []byte) []int16 {
	pcm := make([]int16, len(payload))
	for index, sample := range payload {
		pcm[index] = muLawToPCM(sample)
	}
	return pcm
}

func encodePCMU(pcm []int16) []byte {
	payload := make([]byte, len(pcm))
	for index, sample := range pcm {
		payload[index] = pcmToMuLaw(sample)
	}
	return payload
}

func resamplePCM(pcm []int16, fromRate, toRate int) ([]int16, error) {
	switch {
	case fromRate == toRate:
		return pcm, nil
	case fromRate == 8000 && toRate == 16000:
		return upsamplePCM(pcm), nil
	case fromRate == 16000 && toRate == 8000:
		if len(pcm)%2 != 0 {
			return nil, fmt.Errorf("phone: 16 kHz frame has odd sample count %d", len(pcm))
		}
		return downsamplePCM(pcm), nil
	default:
		return nil, fmt.Errorf("phone: unsupported realtime resampling %d Hz to %d Hz", fromRate, toRate)
	}
}

func upsamplePCM(pcm []int16) []int16 {
	result := make([]int16, len(pcm)*2)
	for index, sample := range pcm {
		next := sample
		if index+1 < len(pcm) {
			next = pcm[index+1]
		}
		result[index*2] = sample
		result[index*2+1] = int16((int(sample) + int(next)) / 2)
	}
	return result
}

func downsamplePCM(pcm []int16) []int16 {
	result := make([]int16, len(pcm)/2)
	for index := range result {
		result[index] = int16((int(pcm[index*2]) + int(pcm[index*2+1])) / 2)
	}
	return result
}
