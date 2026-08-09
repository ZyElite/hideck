package common

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ToStrings converts non-nil IP addresses to their string representations.
func ToStrings(addresses []net.IP) []string {
	result := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if address != nil {
			result = append(result, address.String())
		}
	}
	return result
}

// Plmn3 normalizes a numeric MCC or MNC component to at least three digits.
func Plmn3(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	number, err := strconv.Atoi(value)
	if err != nil || number >= 1000 {
		return value
	}
	return fmt.Sprintf("%03d", number)
}

// IsIPv6AddrString recognizes parsed IPv6 literals and colon-rich host forms
// accepted by the original IMS address handling.
func IsIPv6AddrString(value string) bool {
	trimmed := strings.Trim(strings.TrimSpace(value), "[]")
	address := net.ParseIP(trimmed)
	if address == nil {
		return strings.Count(value, ":") > 1
	}
	return address.To4() == nil
}

// HostHasIP reports whether a host interface owns the supplied IP literal.
func HostHasIP(value string) bool {
	address := net.ParseIP(strings.Trim(strings.TrimSpace(value), "[]"))
	if address == nil {
		return false
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, candidate := range addresses {
			network, ok := candidate.(*net.IPNet)
			if ok && network.IP.Equal(address) {
				return true
			}
		}
	}
	return false
}

// RandomHex returns exactly length lowercase hexadecimal characters.
func RandomHex(length int) string {
	if length <= 0 {
		return ""
	}
	raw := make([]byte, (length+1)/2)
	_, _ = rand.Read(raw)
	encoded := hex.EncodeToString(raw)
	if len(encoded) > length {
		return encoded[:length]
	}
	return encoded
}
