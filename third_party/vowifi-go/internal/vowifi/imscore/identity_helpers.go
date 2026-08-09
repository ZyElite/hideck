package imscore

import "strings"

func pickAOR(cfg IMSConfig) string {
	if value := strings.TrimSpace(cfg.IMPU); value != "" {
		return value
	}
	return strings.TrimSpace(cfg.IMPI)
}

func preferredPublicAOR(primary, associated, fallback string) string {
	return firstNonBlank(primary, associated, fallback)
}
