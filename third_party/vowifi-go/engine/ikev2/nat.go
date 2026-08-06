package ikev2

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"net"
)

// CalculateNATDetectionHash computes the NAT detection payload hash
// (RFC 7296 §2.21):
//
//	hash = PRF(Ni/Nr, <InitiatorSPI> | <ResponderSPI> | <IP address> | <port>)
//
// with the HMAC-SHA1 PRF as used by the original.
func CalculateNATDetectionHash(prfKey []byte, initiatorSPI, responderSPI [8]byte, ip net.IP, port uint16) []byte {
	mac := hmac.New(sha1.New, prfKey)
	mac.Write(initiatorSPI[:])
	mac.Write(responderSPI[:])
	mac.Write(ip)
	var p [2]byte
	binary.BigEndian.PutUint16(p[:], port)
	mac.Write(p[:])
	return mac.Sum(nil)
}
