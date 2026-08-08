package eap

func (a *Attribute) Encode() []byte {
	valueLength := len(a.Value)
	totalLength := 2 + valueLength
	if remainder := totalLength % 4; remainder != 0 {
		totalLength += 4 - remainder
	}
	a.Length = uint8(totalLength / 4)
	result := make([]byte, totalLength)
	result[0] = a.Type
	result[1] = a.Length
	copy(result[2:], a.Value)
	return result
}
