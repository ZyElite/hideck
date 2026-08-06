// Package driver implements the Linux network configuration surface used by the
// SWu session: netlink address/route management, TUN devices and kernel XFRM
// SA/SP installation.
//
// Reconstructed from the decompiled engine/driver. The algorithm mapping
// (xfrm_algo.go) is platform-independent; the netlink/TUN/XFRM implementations
// are Linux-only and compiled behind build tags.
package driver

// IKEv2 transform IDs (RFC 7296 §3.3.2) mapped to kernel XFRM algorithm names.
const (
	// Encryption transforms.
	encrDESCBC      uint16 = 3  // ENCR_3DES
	encrAESCBC      uint16 = 12 // ENCR_AES_CBC
	encrAESGCM16    uint16 = 18 // ENCR_AES_GCM_16
	encrAESGCM8     uint16 = 20 // ENCR_AES_GCM_8
	encrAESCCM8     uint16 = 14 // ENCR_AES_CCM_8
	encrAESCCM12    uint16 = 15 // ENCR_AES_CCM_12
	encrAESCCM16    uint16 = 16 // ENCR_AES_CCM_16
	encrNull        uint16 = 11 // ENCR_NULL
	// Integrity transforms.
	integHMACMD5    uint16 = 1 // AUTH_HMAC_MD5_96
	integHMACSHA1   uint16 = 2 // AUTH_HMAC_SHA1_96
	integHMACSHA256 uint16 = 5 // AUTH_HMAC_SHA2_256_128
	integHMACSHA384 uint16 = 6 // AUTH_HMAC_SHA2_384_192
	integHMACSHA512 uint16 = 7 // AUTH_HMAC_SHA2_512_256
	integAESXCBC    uint16 = 9 // AUTH_AES_XCBC_96
)

// IKEv2AlgToXFRMCrypt maps an IKEv2 encryption transform ID to the kernel XFRM
// cipher algorithm name (e.g. "cbc(aes)"), or "" if unsupported.
func IKEv2AlgToXFRMCrypt(alg uint16) string {
	switch alg {
	case encrDESCBC:
		return "cbc(des3_ede)"
	case encrAESCBC:
		return "cbc(aes)"
	case encrAESGCM16, encrAESGCM8:
		return "rfc4106(gcm(aes))"
	case encrAESCCM8, encrAESCCM12, encrAESCCM16:
		return "rfc4309(ccm(aes))"
	case encrNull:
		return "ecb(cipher_null)"
	default:
		return ""
	}
}

// IKEv2AlgToXFRMAuth maps an IKEv2 integrity transform ID to the kernel XFRM
// authentication algorithm name (e.g. "hmac(sha1)"), or "" if unsupported.
func IKEv2AlgToXFRMAuth(alg uint16) string {
	switch alg {
	case integHMACMD5:
		return "digest_null" // hmac(md5) is not available in all kernels
	case integHMACSHA1:
		return "hmac(sha1)"
	case integHMACSHA256:
		return "hmac(sha256)"
	case integHMACSHA384:
		return "hmac(sha384)"
	case integHMACSHA512:
		return "hmac(sha512)"
	case integAESXCBC:
		return "xcbc(aes)"
	default:
		return ""
	}
}

// IKEv2AlgToXFRMAead maps an IKEv2 AEAD transform ID to the kernel XFRM AEAD
// algorithm name, or "" if the transform is not an AEAD.
func IKEv2AlgToXFRMAead(alg uint16) string {
	switch alg {
	case encrAESGCM16, encrAESGCM8:
		return "rfc4106(gcm(aes))"
	case encrAESCCM8, encrAESCCM12, encrAESCCM16:
		return "rfc4309(ccm(aes))"
	default:
		return ""
	}
}
