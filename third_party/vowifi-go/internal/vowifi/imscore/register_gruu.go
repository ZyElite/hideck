package imscore

import (
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

func (s *Service) recordRegisterGRUU(contact string) {
	public, temporary, err := parseRegisterGRUU(contact)
	if err != nil {
		logging.WarnRate("ims-register-gruu-"+s.DeviceID(), 30*time.Second,
			"IMS REGISTER Contact GRUU parse failed", "device", s.DeviceID(), "err", err)
		return
	}
	s.mu.Lock()
	s.pubGRUU = public
	s.tempGRUU = temporary
	s.mu.Unlock()
}

func parseRegisterGRUU(contact string) (string, string, error) {
	contact = strings.TrimSpace(contact)
	if contact == "" {
		return "", "", nil
	}
	var address sip.Uri
	if _, err := sip.ParseAddressValue(contact, &address, nil); err != nil {
		return "", "", fmt.Errorf("parse Contact address: %w", err)
	}
	public, err := parsedGRUUParam(contact, "pub-gruu")
	if err != nil {
		return "", "", err
	}
	temporary, err := parsedGRUUParam(contact, "temp-gruu")
	if err != nil {
		return "", "", err
	}
	return public, temporary, nil
}

func parsedGRUUParam(contact, name string) (string, error) {
	value, exists := contactParameterValue(contact, name)
	if !exists {
		return "", nil
	}
	value = strings.Trim(strings.TrimSpace(value), "\"")
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("invalid %s parameter", name)
	}
	var uri sip.Uri
	if err := sip.ParseUri(value, &uri); err != nil {
		return "", fmt.Errorf("parse %s URI: %w", name, err)
	}
	if uri.Host == "" || uri.Wildcard {
		return "", fmt.Errorf("invalid %s URI", name)
	}
	return value, nil
}

func contactParameterValue(contact, target string) (string, bool) {
	for index := 0; index < len(contact); {
		separator := nextContactParameter(contact, index)
		if separator < 0 {
			return "", false
		}
		nameStart := separator + 1
		nameEnd := nameStart
		for nameEnd < len(contact) && contact[nameEnd] != '=' && contact[nameEnd] != ';' && contact[nameEnd] != ',' {
			nameEnd++
		}
		name := strings.TrimSpace(contact[nameStart:nameEnd])
		if nameEnd >= len(contact) || contact[nameEnd] != '=' {
			index = nameEnd
			continue
		}
		value, end := contactParameterRawValue(contact, nameEnd+1)
		if strings.EqualFold(name, target) {
			return value, true
		}
		index = end
	}
	return "", false
}

func nextContactParameter(contact string, start int) int {
	quoted, escaped, angleDepth := false, false, 0
	for index := start; index < len(contact); index++ {
		char := contact[index]
		if escaped {
			escaped = false
			continue
		}
		if quoted && char == '\\' {
			escaped = true
			continue
		}
		if char == '"' {
			quoted = !quoted
			continue
		}
		if quoted {
			continue
		}
		switch char {
		case '<':
			angleDepth++
		case '>':
			if angleDepth > 0 {
				angleDepth--
			}
		case ';':
			if angleDepth == 0 {
				return index
			}
		}
	}
	return -1
}

func contactParameterRawValue(contact string, start int) (string, int) {
	start = skipASCIISpace(contact, start)
	if start >= len(contact) || contact[start] != '"' {
		end := strings.IndexAny(contact[start:], ";,")
		if end < 0 {
			return strings.TrimSpace(contact[start:]), len(contact)
		}
		return strings.TrimSpace(contact[start : start+end]), start + end
	}
	var value strings.Builder
	escaped := false
	for index := start + 1; index < len(contact); index++ {
		char := contact[index]
		if escaped {
			value.WriteByte(char)
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if char == '"' {
			return value.String(), index + 1
		}
		value.WriteByte(char)
	}
	return value.String(), len(contact)
}

func skipASCIISpace(value string, index int) int {
	for index < len(value) && (value[index] == ' ' || value[index] == '\t') {
		index++
	}
	return index
}
