package driver

import "fmt"

type XFRMCryptAlgo struct {
	Name    string
	KeyBits int
}

type XFRMAuthAlgo struct {
	Name         string
	KeyBits      int
	TruncateBits int
}

type XFRMAeadAlgo struct {
	Name    string
	KeyBits int
	ICVBits int
}

func IKEv2AlgToXFRMCrypt(algorithmID uint16, keyLengthBits int) (*XFRMCryptAlgo, error) {
	if keyLengthBits == 0 {
		keyLengthBits = 128
	}
	switch algorithmID {
	case 2:
		return &XFRMCryptAlgo{Name: "cbc(des)", KeyBits: 64}, nil
	case 3:
		return &XFRMCryptAlgo{Name: "cbc(des3_ede)", KeyBits: 192}, nil
	case 12:
		return &XFRMCryptAlgo{Name: "cbc(aes)", KeyBits: keyLengthBits}, nil
	case 13:
		return &XFRMCryptAlgo{Name: "rfc3686(ctr(aes))", KeyBits: keyLengthBits}, nil
	default:
		return nil, fmt.Errorf("不支持的 XFRM 加密算法 ID: %d", algorithmID)
	}
}

func IKEv2AlgToXFRMAuth(algorithmID uint16) (*XFRMAuthAlgo, error) {
	switch algorithmID {
	case 1:
		return &XFRMAuthAlgo{Name: "hmac(md5)", KeyBits: 128, TruncateBits: 96}, nil
	case 2:
		return &XFRMAuthAlgo{Name: "hmac(sha1)", KeyBits: 160, TruncateBits: 96}, nil
	case 12:
		return &XFRMAuthAlgo{Name: "hmac(sha256)", KeyBits: 256, TruncateBits: 128}, nil
	case 13:
		return &XFRMAuthAlgo{Name: "hmac(sha384)", KeyBits: 384, TruncateBits: 192}, nil
	case 14:
		return &XFRMAuthAlgo{Name: "hmac(sha512)", KeyBits: 512, TruncateBits: 256}, nil
	default:
		return nil, fmt.Errorf("不支持的 XFRM 完整性算法 ID: %d", algorithmID)
	}
}

func IKEv2AlgToXFRMAead(algorithmID uint16, keyLengthBits int) (*XFRMAeadAlgo, error) {
	if keyLengthBits == 0 {
		keyLengthBits = 128
	}
	name := "rfc4106(gcm(aes))"
	saltBits := 32
	icvBits := 0
	switch algorithmID {
	case 18:
		icvBits = 64
	case 19:
		icvBits = 96
	case 20:
		icvBits = 128
	case 14:
		name, saltBits, icvBits = "rfc4309(ccm(aes))", 24, 64
	case 15:
		name, saltBits, icvBits = "rfc4309(ccm(aes))", 24, 96
	case 16:
		name, saltBits, icvBits = "rfc4309(ccm(aes))", 24, 128
	default:
		return nil, fmt.Errorf("不支持的 XFRM AEAD 算法 ID: %d", algorithmID)
	}
	return &XFRMAeadAlgo{Name: name, KeyBits: keyLengthBits + saltBits, ICVBits: icvBits}, nil
}

func IsAEADAlgorithm(algorithmID uint16) bool {
	switch algorithmID {
	case 14, 15, 16, 18, 19, 20:
		return true
	default:
		return false
	}
}

func IKEv2AlgToXFRMCryptName(algorithmID uint16) string {
	algorithm, err := IKEv2AlgToXFRMCrypt(algorithmID, 0)
	if err != nil {
		return ""
	}
	return algorithm.Name
}

func IKEv2AlgToXFRMAuthName(algorithmID uint16) string {
	algorithm, err := IKEv2AlgToXFRMAuth(algorithmID)
	if err != nil {
		return ""
	}
	return algorithm.Name
}

func IKEv2AlgToXFRMAeadName(algorithmID uint16) string {
	algorithm, err := IKEv2AlgToXFRMAead(algorithmID, 0)
	if err != nil {
		return ""
	}
	return algorithm.Name
}
