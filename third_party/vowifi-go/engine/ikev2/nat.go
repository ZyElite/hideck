package ikev2

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
)

// CalculateNATDetectionHash supports the original RFC 7296 form
// (SPIi, SPIr, IP, port) and the interim keyed compatibility form.
func CalculateNATDetectionHash(arguments ...any) []byte {
	if len(arguments) == 4 {
		spiI, okI := arguments[0].(uint64)
		spiR, okR := arguments[1].(uint64)
		ip, okIP := arguments[2].([]byte)
		port, okPort := arguments[3].(uint16)
		if !okI || !okR || !okIP || !okPort {
			panic("CalculateNATDetectionHash: expected uint64, uint64, []byte, uint16")
		}
		hash := sha1.New()
		writeNATDetectionInput(hash.Write, spiI, spiR, ip, port)
		return hash.Sum(nil)
	}
	if len(arguments) == 5 {
		key, okKey := arguments[0].([]byte)
		spiI, okI := arguments[1].([8]byte)
		spiR, okR := arguments[2].([8]byte)
		ip, okIP := arguments[3].([]byte)
		port, okPort := arguments[4].(uint16)
		if !okKey || !okI || !okR || !okIP || !okPort {
			panic("CalculateNATDetectionHash: invalid keyed compatibility arguments")
		}
		mac := hmac.New(sha1.New, key)
		writeNATDetectionInput(
			mac.Write, binary.BigEndian.Uint64(spiI[:]), binary.BigEndian.Uint64(spiR[:]), ip, port,
		)
		return mac.Sum(nil)
	}
	panic(fmt.Sprintf("CalculateNATDetectionHash: expected 4 or 5 arguments, got %d", len(arguments)))
}

func writeNATDetectionInput(
	write func([]byte) (int, error),
	spiI, spiR uint64,
	ip []byte,
	port uint16,
) {
	var spis [16]byte
	binary.BigEndian.PutUint64(spis[0:8], spiI)
	binary.BigEndian.PutUint64(spis[8:16], spiR)
	var portBytes [2]byte
	binary.BigEndian.PutUint16(portBytes[:], port)
	_, _ = write(spis[:])
	_, _ = write(ip)
	_, _ = write(portBytes[:])
}

func CreateNATDetectionNotify(notifyType uint16, hash []byte) *EncryptedPayloadNotify {
	return &EncryptedPayloadNotify{ProtocolID: ProtoIKE, NotifyType: notifyType, NotifyData: hash}
}
