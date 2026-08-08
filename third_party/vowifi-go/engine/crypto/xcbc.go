package crypto

import "crypto/aes"

const aesXCBCBlockSize = aes.BlockSize

func aesXCBCPRF128(key, data []byte) []byte {
	return aesXCBCMAC(normalizeXCBCPRFKey(key), data)
}

func normalizeXCBCPRFKey(key []byte) []byte {
	switch {
	case len(key) == aesXCBCBlockSize:
		return append([]byte(nil), key...)
	case len(key) < aesXCBCBlockSize:
		normalized := make([]byte, aesXCBCBlockSize)
		copy(normalized, key)
		return normalized
	default:
		return aesXCBCMAC(make([]byte, aesXCBCBlockSize), key)
	}
}

func aesXCBCMAC(key, data []byte) []byte {
	if len(key) != aesXCBCBlockSize {
		panic("AES-XCBC requires 16-byte key")
	}
	baseCipher, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	k1 := encryptedRepeatedBlock(baseCipher, 0x01)
	k2 := encryptedRepeatedBlock(baseCipher, 0x02)
	k3 := encryptedRepeatedBlock(baseCipher, 0x03)
	workCipher, err := aes.NewCipher(k1)
	if err != nil {
		panic(err)
	}
	return computeXCBCBlocks(workCipher, k2, k3, data)
}

func encryptedRepeatedBlock(block interface{ Encrypt(dst, src []byte) }, value byte) []byte {
	plain := make([]byte, aesXCBCBlockSize)
	for i := range plain {
		plain[i] = value
	}
	result := make([]byte, aesXCBCBlockSize)
	block.Encrypt(result, plain)
	return result
}

func computeXCBCBlocks(block interface{ Encrypt(dst, src []byte) }, k2, k3, data []byte) []byte {
	state := make([]byte, aesXCBCBlockSize)
	if len(data) == 0 {
		last := make([]byte, aesXCBCBlockSize)
		last[0] = 0x80
		xorBlock(last, k3)
		block.Encrypt(state, last)
		return state
	}
	blocks := (len(data) + aesXCBCBlockSize - 1) / aesXCBCBlockSize
	for i := 0; i < blocks-1; i++ {
		processXCBCBlock(block, state, data[i*aesXCBCBlockSize:(i+1)*aesXCBCBlockSize])
	}
	finishXCBC(block, state, k2, k3, data[(blocks-1)*aesXCBCBlockSize:])
	return state
}

func processXCBCBlock(block interface{ Encrypt(dst, src []byte) }, state, data []byte) {
	input := append([]byte(nil), data...)
	xorBlock(input, state)
	block.Encrypt(state, input)
}

func finishXCBC(block interface{ Encrypt(dst, src []byte) }, state, k2, k3, remaining []byte) {
	last := make([]byte, aesXCBCBlockSize)
	copy(last, remaining)
	if len(remaining) == aesXCBCBlockSize {
		xorBlock(last, k2)
	} else {
		last[len(remaining)] = 0x80
		xorBlock(last, k3)
	}
	xorBlock(last, state)
	block.Encrypt(state, last)
}

func xorBlock(dst, src []byte) {
	for i := range dst {
		dst[i] ^= src[i]
	}
}
