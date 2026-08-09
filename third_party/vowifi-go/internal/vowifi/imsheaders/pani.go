package imsheaders

import "strings"

const defaultPAccessNetworkInfo = "IEEE-802.11; i-wlan-node-id=000000000000"

// PAccessNetworkInfo normalizes the configured P-Access-Network-Info value.
func PAccessNetworkInfo(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultPAccessNetworkInfo
	}
	return value
}
