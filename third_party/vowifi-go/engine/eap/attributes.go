package eap

import (
	"errors"
	"fmt"
)

// ParseAttributes parses EAP-AKA attributes from b (RFC 4187 §9). Each
// attribute is Type(1) | Length(1, in 4-byte words) | Value(Length*4 - 2).
// Attributes are returned in a map keyed by attribute type.
func ParseAttributes(b []byte, origCap int) (map[byte]*EAPAttribute, error) {
	attrs := make(map[byte]*EAPAttribute)
	i := 0
	for i+2 <= len(b) {
		attrType := b[i]
		words := int(b[i+1])
		if words == 0 {
			return nil, errors.New("eap: zero attribute length")
		}
		end := i + words*4
		if end > len(b) {
			return nil, fmt.Errorf("eap: attribute %d length %d exceeds buffer", attrType, words)
		}
		attrs[attrType] = &EAPAttribute{
			Type:   attrType,
			Length: b[i+1],
			Value:  append([]byte{}, b[i+2:end]...),
		}
		i = end
	}
	return attrs, nil
}