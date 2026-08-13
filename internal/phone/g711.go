package phone

func muLawToPCM(value byte) int16 {
	value = ^value
	magnitude := ((int(value&0x0f) << 3) + 0x84) << ((value & 0x70) >> 4)
	if value&0x80 != 0 {
		return int16(0x84 - magnitude)
	}
	return int16(magnitude - 0x84)
}

func pcmToMuLaw(sample int16) byte {
	value := int(sample)
	sign := byte(0)
	if value < 0 {
		sign = 0x80
		value = -value
	}
	if value > 32635 {
		value = 32635
	}
	value += 0x84
	exponent := 7
	for mask := 0x4000; exponent > 0 && value&mask == 0; mask >>= 1 {
		exponent--
	}
	mantissa := (value >> (exponent + 3)) & 0x0f
	return ^(sign | byte(exponent<<4) | byte(mantissa))
}

func aLawToPCM(value byte) int16 {
	value ^= 0x55
	magnitude := int(value&0x0f) << 4
	exponent := int((value & 0x70) >> 4)
	if exponent == 0 {
		magnitude += 8
	} else {
		magnitude = (magnitude + 0x108) << (exponent - 1)
	}
	if value&0x80 == 0 {
		magnitude = -magnitude
	}
	return int16(magnitude)
}

func pcmToALaw(sample int16) byte {
	value := int(sample)
	sign := byte(0x80)
	if value < 0 {
		sign = 0
		value = -value - 1
	}
	if value > 32767 {
		value = 32767
	}
	var encoded byte
	if value < 256 {
		encoded = byte(value >> 4)
	} else {
		exponent := 1
		for threshold := 0x200; exponent < 7 && value >= threshold; threshold <<= 1 {
			exponent++
		}
		encoded = byte(exponent<<4) | byte((value>>(exponent+3))&0x0f)
	}
	return (encoded | sign) ^ 0x55
}

func transcodeG711(payload []byte, from, to string) {
	if from == to {
		return
	}
	for index, value := range payload {
		if from == "PCMA" {
			payload[index] = pcmToMuLaw(aLawToPCM(value))
		} else {
			payload[index] = pcmToALaw(muLawToPCM(value))
		}
	}
}
