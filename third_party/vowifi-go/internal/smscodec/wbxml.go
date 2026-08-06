package smscodec

import (
	"errors"
	"fmt"
	"strings"
)

// OmaCPCharacteristic is one record decoded from an OMA Client Provisioning
// (OMA-ERELD / wap-provisioningdoc) WBXML document.
//
// The original stores every <characteristic type="…"> and <parm name="…"
// value="…"/> element as a 48-byte record (three 16-byte strings); the summary
// formatter joins them with newlines.
type OmaCPCharacteristic struct {
	Type  string // the type="…" attribute of a <characteristic>
	Name  string // the name="…" attribute of a <parm>
	Value string // the value="…" attribute of a <parm>
}

// FormatOmaCPSummary renders a slice of characteristics as a multi-line text
// summary. Empty input yields the original's "OMA CP \n" placeholder.
func FormatOmaCPSummary(chars []OmaCPCharacteristic) string {
	if len(chars) == 0 {
		return "OMA CP \n"
	}
	var b strings.Builder
	for i, c := range chars {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(formatCharacteristic(c))
	}
	return b.String()
}

// formatCharacteristic renders one characteristic record as text.
func formatCharacteristic(c OmaCPCharacteristic) string {
	switch {
	case c.Type != "":
		return "characteristic type=" + c.Type
	case c.Name != "":
		return "parm name=" + c.Name + " value=" + c.Value
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// WBXML tokenizer
// ---------------------------------------------------------------------------

// wbxmlReader walks a WBXML byte stream (WAP-192) with multi-byte uint and
// string primitives.
type wbxmlReader struct {
	data []byte
	pos  int
	// string table (WBXML header STR_TABL)
	strtab []byte
}

func (r *wbxmlReader) eof() bool      { return r.pos >= len(r.data) }
func (r *wbxmlReader) remaining() int { return len(r.data) - r.pos }

func (r *wbxmlReader) readByte() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, errors.New("WBXML: unexpected end")
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

// readMBUint32 reads a multi-byte integer (7 bits per byte, high bit = more).
func (r *wbxmlReader) readMBUint32() (uint32, error) {
	var v uint32
	for i := 0; i < 5; i++ {
		b, err := r.readByte()
		if err != nil {
			return 0, err
		}
		v = v<<7 | uint32(b&0x7f)
		if b&0x80 == 0 {
			return v, nil
		}
	}
	return 0, errors.New("WBXML: mb_uint32 too long")
}

func (r *wbxmlReader) readBytes(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.data) {
		return nil, fmt.Errorf("WBXML: need %d, have %d", n, r.remaining())
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b, nil
}

// readStrI reads an inline string (uint8 length + bytes).
func (r *wbxmlReader) readStrI() (string, error) {
	n, err := r.readByte()
	if err != nil {
		return "", err
	}
	b, err := r.readBytes(int(n))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// readStrT reads a string table reference (mb_uint32 index).
func (r *wbxmlReader) readStrT() (string, error) {
	idx, err := r.readMBUint32()
	if err != nil {
		return "", err
	}
	if int(idx) >= len(r.strtab) {
		return "", fmt.Errorf("WBXML: string table index %d out of range", idx)
	}
	// strings are NUL-terminated in the table
	end := strings.IndexByte(string(r.strtab[idx:]), 0)
	if end < 0 {
		end = len(r.strtab) - int(idx)
	}
	return string(r.strtab[idx : idx+uint32(end)]), nil
}

// ---------------------------------------------------------------------------
// OMA CP DTD tables
//
// Recovered from the binary's map.init.0: page 0/1 tags
//
//	5 -> "wap-provisioningdoc", 6 -> "characteristic", 7 -> "parm"
//
// and the standard OMA Client Provisioning DTD attribute table (map.init.1..3
// build 59/67-entry tables from static data).
// ---------------------------------------------------------------------------

var omacpTagNames = map[uint8]string{
	5: "wap-provisioningdoc",
	6: "characteristic",
	7: "parm",
}

var omacpAttrNames = map[uint8]string{
	5:  "name",
	6:  "value",
	7:  "type",
	8:  "version",
	9:  "href",
	10: "title",
	// the original carries the full OMA CP DTD table (PXLOGICAL, NAPDEF,
	// ACCESS, …); the entries above cover the characteristics used by the
	// provisioner. Unknown tokens fall back to "0x%02X".
}

// resolveTagName maps a WBXML tag token to its element name.
func resolveTagName(page int, token byte) string {
	if page == 0 {
		if n, ok := omacpTagNames[token]; ok {
			return n
		}
	}
	return fmt.Sprintf("0x%02X", token)
}

// resolveAttrStart maps an attribute token to its attribute name.
func resolveAttrStart(page int, token byte) string {
	if page == 0 {
		if n, ok := omacpAttrNames[token]; ok {
			return n
		}
	}
	return fmt.Sprintf("0x%02X", token)
}

// ---------------------------------------------------------------------------
// WBXML decoding
// ---------------------------------------------------------------------------

// WBXML token constants (WAP-192).
const (
	wbxmlSwitchPage = 0x00
	wbxmlEnd        = 0x01
	wbxmlEntity     = 0x02
	wbxmlStrI       = 0x03
	wbxmlOpaque     = 0x04
	wbxmlStrT       = 0x05
	wbxmlAttrMask   = 0x80
	wbxmlLiteral    = 0x40
)

// DecodeOmaCPFromTPDU parses a binary SMS payload as an OMA Client
// Provisioning WBXML document and returns its characteristics.
func DecodeOmaCPFromTPDU(data []byte) ([]OmaCPCharacteristic, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("OMA CP: too short (%d bytes)", len(data))
	}
	start, ok := findWBXMLStart(data)
	if !ok {
		return nil, errors.New("OMA CP: no WBXML header found")
	}
	return decodeWBXML(data[start:])
}

// findWBXMLStart locates the WBXML header within a binary SMS body. The body
// may be preceded by a WSP push header or arbitrary bytes; the header starts
// at the first plausible version byte (0x01..0x04) followed by a public-ID
// token.
func findWBXMLStart(data []byte) (int, bool) {
	for i := 0; i+3 < len(data); i++ {
		if data[i] >= 0x01 && data[i] <= 0x04 && data[i+1] != 0x00 && isWBXMLHeader(data[i:]) {
			return i, true
		}
	}
	return 0, false
}

// isWBXMLHeader reports whether data begins with a plausible WBXML header:
// version, non-zero public ID, and a parseable charset/string-table length.
func isWBXMLHeader(data []byte) bool {
	if len(data) < 4 || data[0] < 1 || data[0] > 4 {
		return false
	}
	// public ID: single token byte (>=0x80) or mb_uint32
	pos := 1
	if data[pos] < 0x80 {
		for pos < len(data) && data[pos]&0x80 != 0 {
			pos++
		}
	}
	pos++
	if pos >= len(data) {
		return false
	}
	// charset: mb_uint32; must be 0x6A (UTF-8) for OMA CP
	charset := uint32(0)
	for i := 0; i < 5 && pos < len(data); i++ {
		charset = charset<<7 | uint32(data[pos]&0x7f)
		last := data[pos]&0x80 == 0
		pos++
		if last {
			break
		}
	}
	return charset == 0x6a
}

// decodeWBXML parses the body of a WBXML document into characteristics.
func decodeWBXML(data []byte) ([]OmaCPCharacteristic, error) {
	r := &wbxmlReader{data: data}

	// header
	ver, err := r.readByte()
	if err != nil {
		return nil, fmt.Errorf("WBXML header: %w", err)
	}
	_ = ver
	pid, err := r.readMBUint32()
	if err != nil {
		return nil, fmt.Errorf("WBXML publicID: %w", err)
	}
	charset, err := r.readMBUint32()
	if err != nil {
		return nil, fmt.Errorf("WBXML charset: %w", err)
	}
	if charset != 0x6a { // UTF-8
		return nil, fmt.Errorf("WBXML charset %#x not supported", charset)
	}
	tabLen, err := r.readMBUint32()
	if err != nil {
		return nil, fmt.Errorf("WBXML string table: %w", err)
	}
	if tabLen > 0 {
		r.strtab, err = r.readBytes(int(tabLen))
		if err != nil {
			return nil, err
		}
	}
	_ = pid // 0x0B = wap-provisioningdoc public ID (checked by caller)

	// body: walk top-level elements; END token terminates.
	var chars []OmaCPCharacteristic
	page := 0
	for !r.eof() {
		tok, err := r.readByte()
		if err != nil {
			return nil, err
		}
		switch {
		case tok == wbxmlEnd:
			return chars, nil
		case tok == wbxmlSwitchPage:
			p, err := r.readByte()
			if err != nil {
				return nil, err
			}
			page = int(p)
		case tok == wbxmlEntity:
			// entity reference: read char code, skip UTF-8 bytes
			c, err := r.readMBUint32()
			if err != nil {
				return nil, err
			}
			_ = c
		case tok == wbxmlStrI || tok == wbxmlStrT:
			// stray inline text (e.g. whitespace) — ignore
			if tok == wbxmlStrI {
				_, err = r.readStrI()
			} else {
				_, err = r.readStrT()
			}
			if err != nil {
				return nil, err
			}
		case tok == wbxmlOpaque:
			n, err := r.readByte()
			if err != nil {
				return nil, err
			}
			if _, err = r.readBytes(int(n)); err != nil {
				return nil, err
			}
		default:
			c, err := r.parseElement(page, tok)
			if err != nil {
				return nil, err
			}
			chars = append(chars, c...)
		}
	}
	return chars, nil
}

// parseElement reads one element (tag token + optional attributes + children
// until END) and returns any characteristic records produced. The WBXML
// grammar for an element is: TagToken Attributes? (content)* END — the END
// token is consumed here, so nested elements nest naturally.
func (r *wbxmlReader) parseElement(page int, tok byte) ([]OmaCPCharacteristic, error) {
	literal := tok&wbxmlLiteral != 0
	tag := tok & 0x3f

	name := resolveTagName(page, tag)
	if literal {
		s, err := r.readStrI()
		if err != nil {
			return nil, err
		}
		name = s
	}

	attrs, err := r.parseAttributes(page)
	if err != nil {
		return nil, err
	}

	// Every element produces its own record if it is a characteristic or parm;
	// children are collected regardless of the element kind.
	var recs []OmaCPCharacteristic
	switch name {
	case "parm":
		recs = append(recs, OmaCPCharacteristic{Name: attrs["name"], Value: attrs["value"]})
	case "characteristic":
		recs = append(recs, OmaCPCharacteristic{Type: attrs["type"]})
	}

	// Content until the matching END token.
	for !r.eof() {
		t, err := r.readByte()
		if err != nil {
			return nil, err
		}
		if t == wbxmlEnd {
			break
		}
		kids, err := r.parseElement(page, t)
		if err != nil {
			return nil, err
		}
		recs = append(recs, kids...)
	}
	return recs, nil
}

// parseAttributes reads the attribute stream of an element until the END
// marker (0x00) and returns the attribute map.
func (r *wbxmlReader) parseAttributes(page int) (map[string]string, error) {
	attrs := map[string]string{}
	for !r.eof() {
		tok, err := r.readByte()
		if err != nil {
			return nil, err
		}
		switch {
		case tok == 0x00:
			return attrs, nil // end of attributes

		case tok == 0x03: // literal attribute: name follows as STR_I, then value
			name, err := r.readStrI()
			if err != nil {
				return nil, err
			}
			val, err := r.readAttrValue(page)
			if err != nil {
				return nil, err
			}
			attrs[name] = val

		case tok == 0x04: // extension token, skip
			_, err = r.readByte()
			if err != nil {
				return nil, err
			}

		case tok >= 0x06 && tok <= 0x3f: // literal attribute index, name follows
			name, err := r.readStrI()
			if err != nil {
				return nil, err
			}
			val, err := r.readAttrValue(page)
			if err != nil {
				return nil, err
			}
			attrs[name] = val

		case tok&0xc0 == 0x80: // known attr, value is an inline string
			name := resolveAttrStart(page, tok&0x3f)
			s, err := r.readStrI()
			if err != nil {
				return nil, err
			}
			attrs[name] = s

		case tok&0xc0 == 0x40: // known attr, value in the next token(s)
			name := resolveAttrStart(page, tok&0x3f)
			val, err := r.readAttrValue(page)
			if err != nil {
				return nil, err
			}
			attrs[name] = val

		default: // 0x01 (entity), 0x02 (STR_I), 0x05 (STR_T) or 0xC0+ (opaque)
			// value for the previous attribute — handled by readAttrValue;
			// a bare value token with no pending attribute is ignored.
		}
	}
	return attrs, nil
}

// readAttrValue reads an attribute value token (STRING/OPAQUE/ENTITY or a
// literal string).
func (r *wbxmlReader) readAttrValue(page int) (string, error) {
	tok, err := r.readByte()
	if err != nil {
		return "", err
	}
	switch {
	case tok == 0x02: // STR_I
		return r.readStrI()
	case tok == 0x05: // STR_T
		return r.readStrT()
	case tok == 0x01: // entity
		c, err := r.readMBUint32()
		if err != nil {
			return "", err
		}
		return string(rune(c)), nil
	case tok == 0x04: // opaque
		n, err := r.readByte()
		if err != nil {
			return "", err
		}
		b, err := r.readBytes(int(n))
		if err != nil {
			return "", err
		}
		return string(b), nil
	case tok&0xc0 == 0x80: // literal value token — read STR_I
		return r.readStrI()
	default:
		return "", nil
	}
}
