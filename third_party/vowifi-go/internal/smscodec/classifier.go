package smscodec

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/warthog618/sms/encoding/tpdu"
)

// WAP push port numbers carried in the TP-UDH destination-port IE.
const (
	wapPushPort    = 0x0b84 // WSP application port (push)
	wapPushPortAlt = 0x0b85
	udhDestPortIE  = 0x05
	udhConcatIE    = 0x00 // 8-bit concatenation
	udhConcatIE16  = 0x08 // 16-bit concatenation
)

// BinaryClassification describes the human-readable result of classifying a
// binary SMS (the output of formatBinaryClassification in the original).
type BinaryClassification struct {
	Kind        string // "MMS Notification", "SIM OTA 23.048", "OMA CP", "WAP Push", …
	ContentType string // normalized content type
	URL         string // content-location / href when present
	Raw         string // printable hint of the payload
}

func (c BinaryClassification) String() string {
	var b strings.Builder
	b.WriteString(c.Kind)
	if c.ContentType != "" {
		fmt.Fprintf(&b, " (%s)", c.ContentType)
	}
	if c.URL != "" {
		fmt.Fprintf(&b, " url=%s", c.URL)
	}
	if c.Raw != "" {
		fmt.Fprintf(&b, " [%s]", c.Raw)
	}
	return b.String()
}

// classifyBinarySMS inspects an 8-bit (binary) decoded TPDU and returns a
// human-readable classification. The ok result reports whether the message
// was recognised as a structured binary SMS.
func classifyBinarySMS(t *tpdu.TPDU) (string, bool) {
	// 8-bit data: DCS 0x04/0x06 (general 8-bit class 0/1) or the 0xF4..0xFF
	// message-class range with the 8-bit indicator (bit 2) set.
	if !isBinaryDCS(t.DCS) {
		return "", false
	}
	ports := parseUDHPorts(t.UDH)
	data := []byte(t.UD)

	switch {
	case ports.DestPort == wapPushPort || ports.DestPort == wapPushPortAlt:
		cls := parseWSPPush(data)
		return formatBinaryClassification(cls, t), true

	case isLikelySIMOTA(data):
		cls := BinaryClassification{Kind: "SIM OTA 23.048"}
		cls.Raw = extractPrintableHint(data)
		return cls.String(), true

	case isLikelySIWBXML(data) || isLikelySLWBXML(data) || isLikelyMMSContentType(data):
		cls := classifyContent(data)
		return cls.String(), true
	}
	return "", false
}

func isBinaryDCS(dcs tpdu.DCS) bool {
	b := byte(dcs)
	return b == 0x04 || b == 0x06 || (b&0xf0 == 0xf0 && b&0x04 != 0)
}

// UDHPorts carries the concatenation and destination-port info from a TPDU
// user data header.
type UDHPorts struct {
	DestPort    int
	ConcatRef   int
	ConcatTotal int
	ConcatSeq   int
}

// parseUDHPorts extracts the destination port and concatenation info from the
// TP-UDH information elements.
func parseUDHPorts(udh tpdu.UserDataHeader) UDHPorts {
	var p UDHPorts
	for _, ie := range udh {
		d := ie.Data
		switch ie.ID {
		case udhDestPortIE:
			if len(d) >= 2 {
				p.DestPort = int(binary.BigEndian.Uint16(d))
			}
		case udhConcatIE:
			if len(d) >= 3 {
				p.ConcatRef, p.ConcatTotal, p.ConcatSeq = int(d[0]), int(d[1]), int(d[2])
			}
		case udhConcatIE16:
			if len(d) >= 4 {
				p.ConcatRef = int(binary.BigEndian.Uint16(d))
				p.ConcatTotal, p.ConcatSeq = int(d[2]), int(d[3])
			}
		}
	}
	return p
}

// ---------------------------------------------------------------------------
// WSP push parsing (WAP-230)
// ---------------------------------------------------------------------------

// parseWSPPush parses the start of a WSP push payload and returns its
// classification. The first bytes are:
//
//	[0]  TID (transaction id)
//	[1]  PDU type (0x06 = PUSH)
//	[2:] headers: content-type (token or length-prefixed), then fields.
func parseWSPPush(data []byte) BinaryClassification {
	cls := BinaryClassification{Kind: "WAP Push"}
	if len(data) < 2 {
		cls.Raw = extractPrintableHint(data)
		return cls
	}
	pos := 2
	// content-type: a well-known token byte, or a length-prefixed string.
	// The well-known WSP content-type tokens (application/vnd.wap.mms-message
	// etc.) live in the 0x20..0x3F range; a byte that maps to one is a token.
	if pos < len(data) {
		if ct, ok := knownWSPContentType(data[pos]); ok {
			cls.ContentType = ct
			pos++
		} else {
			n := int(data[pos])
			pos++
			if pos+n <= len(data) {
				cls.ContentType = normalizeContentType(string(data[pos : pos+n]))
				pos += n
			}
		}
	}
	// scan remaining header fields for content-location / X-Wap-Application-Id
	cls.URL = scanWSPHeaders(data[pos:])
	if cls.URL == "" {
		cls.URL = extractURL(data)
	}
	return cls
}

// scanWSPHeaders walks WSP header fields looking for a content location.
func scanWSPHeaders(data []byte) string {
	i := 0
	for i+1 < len(data) {
		// header field: name (token byte, or length-prefixed string), value
		var name string
		if data[i] >= 0x80 {
			name = mapWSPHeaderName(data[i])
			i++
		} else {
			n := int(data[i])
			i++
			if i+n > len(data) {
				break
			}
			name = string(data[i : i+n])
			i += n
		}
		if i >= len(data) {
			break
		}
		var val string
		if data[i] >= 0x80 {
			// token value (e.g. well-known header value) — no URL
			i++
			continue
		}
		n := int(data[i])
		i++
		if i+n > len(data) {
			break
		}
		val = string(data[i : i+n])
		i += n
		if strings.EqualFold(name, "content-location") {
			return val
		}
	}
	return ""
}

func mapWSPHeaderName(tok byte) string {
	switch tok & 0x7f {
	case 0x0d:
		return "content-location"
	default:
		return fmt.Sprintf("hdr-0x%02x", tok&0x7f)
	}
}

// knownWSPContentType reports whether b is a well-known WSP content-type token
// and, if so, its media type.
func knownWSPContentType(b byte) (string, bool) {
	switch b {
	case 0x2c:
		return "application/vnd.wap.mms-message", true
	case 0x2e:
		return "application/vnd.wap.connectivity-wbxml", true
	case 0x30:
		return "application/vnd.wap.sic", true
	case 0x31:
		return "application/vnd.wap.slc", true
	case 0x33:
		return "application/vnd.wap.si", true
	case 0x34:
		return "application/vnd.wap.sl", true
	case 0x37:
		return "application/vnd.wap.syncml-notification", true
	default:
		return "", false
	}
}

// mapWSPContentTypeToken maps the well-known WSP content-type tokens.
func mapWSPContentTypeToken(tok byte) string {
	if ct, ok := knownWSPContentType(tok); ok {
		return ct
	}
	return fmt.Sprintf("application/x-wap-token-0x%02x", tok)
}

// parseWSPContentType parses a raw content-type string.
func parseWSPContentType(s string) string { return normalizeContentType(s) }

// normalizeContentType trims parameters (e.g. ";charset=…") and whitespace.
func normalizeContentType(s string) string {
	if i := strings.IndexByte(s, ';'); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// classifyContent decides MMS / SIS / OMA CP based on content type.
func classifyContent(data []byte) BinaryClassification {
	if isLikelyMMSContentType(data) {
		cls := BinaryClassification{Kind: "MMS Notification"}
		cls.URL = extractURL(data)
		return cls
	}
	if isLikelySLWBXML(data) || isLikelySIWBXML(data) {
		cls := BinaryClassification{Kind: "SIS Download"}
		cls.URL = extractURL(data)
		return cls
	}
	cls := BinaryClassification{Kind: "OMA CP"}
	cls.Raw = extractPrintableHint(data)
	return cls
}

// isLikelyMMSContentType reports whether the payload declares the MMS message
// content type.
func isLikelyMMSContentType(data []byte) bool {
	s := string(data)
	low := strings.ToLower(s)
	return strings.Contains(low, "application/vnd.wap.mms-message") ||
		strings.Contains(low, "application/vnd.wap.multipart.related")
}

// isLikelySIWBXML / isLikelySLWBXML report whether the body looks like the
// corresponding WAP service indication / loading WBXML document.
func isLikelySIWBXML(data []byte) bool { return looksLikeWAPWBXML(data, "si") }
func isLikelySLWBXML(data []byte) bool { return looksLikeWAPWBXML(data, "sl") }
func isLikelySLContentType(s string) bool {
	s = normalizeContentType(s)
	return s == "application/vnd.wap.sl" || s == "application/vnd.wap.slc"
}

func looksLikeWAPWBXML(data []byte, doc string) bool {
	if !isWBXMLHeader(data) {
		return false
	}
	s := string(data)
	low := strings.ToLower(s)
	return strings.Contains(low, "si") && doc == "si" ||
		strings.Contains(low, "sl") && doc == "sl" ||
		strings.Contains(low, doc)
}

// wbxmlPublicID returns the known WBXML public ID tokens as strings.
func wbxmlPublicID(pid uint32) string {
	switch pid {
	case 0x0b:
		return "OMA CP (wap-provisioningdoc)"
	case 0x02:
		return "WAP SI"
	case 0x03:
		return "WAP SL"
	default:
		return fmt.Sprintf("0x%X", pid)
	}
}

// isLikelySIMOTA reports whether the payload resembles a SIM OTA (STK)
// proactive-command message: a BER-TLV with tag 0xD0 (proactive command)
// inside 0x81/0x82 envelope.
func isLikelySIMOTA(data []byte) bool {
	return len(data) > 2 && data[0] == 0xd0 && (data[1]&0x80) != 0
}

// addMMSHints appends MMS-specific hints (subject/size) to the classification.
func addMMSHints(cls *BinaryClassification, data []byte) {
	cls.URL = extractURL(data)
	if s := extractTaggedUint(data, "size"); s > 0 {
		cls.Raw = fmt.Sprintf("%d bytes", s)
	}
}

// addSISLHints appends SIS-specific hints to the classification.
func addSISLHints(cls *BinaryClassification, data []byte) {
	cls.URL = extractURL(data)
	cls.Raw = extractPrintableHint(data)
}

// extractURL pulls a URL out of a WAP push body (content-location / href).
func extractURL(data []byte) string {
	s := string(data)
	low := strings.ToLower(s)
	for _, key := range []string{"content-location:", "href=", "url=", "si" + string([]byte{0})} {
		if i := strings.Index(low, key); i >= 0 {
			rest := s[i+len(key):]
			end := strings.IndexAny(rest, "\x00\r\n\x01")
			if end < 0 {
				end = len(rest)
			}
			if end > 0 {
				u := strings.TrimSpace(rest[:end])
				if strings.Contains(u, "://") || strings.HasPrefix(u, "http") {
					return u
				}
			}
		}
	}
	return ""
}

// extractPrintableHint renders the printable run of the payload for display.
func extractPrintableHint(data []byte) string {
	var b strings.Builder
	for _, c := range data {
		if c >= 0x20 && c < 0x7f {
			b.WriteByte(c)
		} else if c == '\n' {
			b.WriteByte(' ')
		} else {
			if b.Len() > 0 {
				break // stop at first binary run
			}
		}
	}
	if b.Len() > 48 {
		return b.String()[:48] + "…"
	}
	return b.String()
}

// extractTaggedUint finds "<name>N</name>" style unsigned ints in the payload.
func extractTaggedUint(data []byte, tag string) uint64 {
	s := string(data)
	low := strings.ToLower(s)
	open := "<" + strings.ToLower(tag) + ">"
	close_ := "</" + strings.ToLower(tag) + ">"
	if i := strings.Index(low, open); i >= 0 {
		start := i + len(open)
		if j := strings.Index(low[start:], close_); j >= 0 {
			var v uint64
			for _, c := range s[start : start+j] {
				if c < '0' || c > '9' {
					break
				}
				v = v*10 + uint64(c-'0')
			}
			return v
		}
	}
	return 0
}

// formatBinaryClassification renders the final summary of a classification.
func formatBinaryClassification(cls BinaryClassification, t *tpdu.TPDU) string {
	switch {
	case cls.Kind == "WAP Push" && strings.Contains(cls.ContentType, "mms"):
		cls.Kind = "MMS Notification"
	case strings.Contains(cls.ContentType, "connectivity-wbxml"):
		cls.Kind = "OMA CP"
	case strings.Contains(cls.ContentType, "sic") || strings.Contains(cls.ContentType, "slc"):
		cls.Kind = "SIS Download"
	}
	return cls.String()
}
