package common

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
)

// Stringify preserves the generic stringer projection added after v1.5.5.
func Stringify[T fmt.Stringer](items []T) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.String())
	}
	return result
}

// JoinPLMN preserves the two-component PLMN formatter added after v1.5.5.
func JoinPLMN(mcc, mnc string) string {
	if len(mnc) == 2 {
		mnc = "0" + mnc
	}
	return mcc + mnc
}

// HostHasParsedIP preserves the parsed-IP host lookup added after v1.5.5.
func HostHasParsedIP(address net.IP) bool {
	return address != nil && HostHasIP(address.String())
}

// RandomHexBytes preserves the byte-counting random helper added after v1.5.5.
func RandomHexBytes(length int) string {
	raw := make([]byte, length)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)
}
