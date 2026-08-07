// Package profile reads the ISIM/USIM identity (IMPI, IMPU, domain) from the
// SIM via logical-channel APDU and resolves the device IMEI / user agent.
//
// Reconstructed from the decompiled internal/vowifi/profile.
package profile

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// AKAAppPreference selects the SIM application used for AKA.
type AKAAppPreference string

const (
	AKAAppPreferenceISIMStrict AKAAppPreference = "isim_strict"
	AKAAppPreferenceISIM       AKAAppPreference = "isim"
	AKAAppPreferenceUSIMStrict AKAAppPreference = "usim_strict"
	AKAAppPreferenceUSIM       AKAAppPreference = "usim"
)

// NormalizeAKAApp normalizes an AKA app preference string.
func NormalizeAKAApp(s string) AKAAppPreference {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "isim_strict":
		return AKAAppPreferenceISIMStrict
	case "isim":
		return AKAAppPreferenceISIM
	case "usim":
		return AKAAppPreferenceUSIM
	case "usim_strict":
		return AKAAppPreferenceUSIMStrict
	default:
		return AKAAppPreferenceISIM
	}
}

// AuthPlan is the resolved authentication plan for a session.
type AuthPlan struct {
	AKAApp AKAAppPreference
	// ISIMAvailable / USIMAvailable report which applications are present.
	ISIMAvailable bool
	USIMAvailable bool
}

// IsZero reports whether the plan is empty.
func (p *AuthPlan) IsZero() bool {
	return p == nil || (!p.ISIMAvailable && !p.USIMAvailable)
}

// Normalize fills defaults in the plan.
func (p *AuthPlan) Normalize() {
	if p == nil {
		return
	}
	if p.AKAApp == "" {
		if p.ISIMAvailable {
			p.AKAApp = AKAAppPreferenceISIM
		} else {
			p.AKAApp = AKAAppPreferenceUSIM
		}
	}
}

// EffectiveAuthPlan returns the effective auth plan for a prepared session.
func (p *PreparedSession) EffectiveAuthPlan() AuthPlan {
	if p == nil {
		return AuthPlan{}
	}
	return p.AuthPlan
}

// PreparedSession is the prepared IMS session.
type PreparedSession struct {
	// Profile is the SIM profile.
	Profile Profile
	// AuthPlan is the resolved authentication plan.
	AuthPlan AuthPlan
	// IMSIdentity is the read ISIM identity.
	IMSIdentity IMSIdentity
}

// Profile is the SIM profile.
type Profile struct {
	IMSI string
	MCC  string
	MNC  string
	IMEI string
	SMSC string
}

// IMSIdentity is the ISIM identity.
type IMSIdentity struct {
	IMPI   string
	IMPU   []string
	Domain string
}

// Normalize normalizes a profile.
func Normalize(p Profile) Profile {
	p.IMSI = strings.TrimSpace(p.IMSI)
	p.IMEI = strings.TrimSpace(p.IMEI)
	if p.MCC == "" && len(p.IMSI) >= 3 {
		p.MCC = p.IMSI[:3]
	}
	if p.MNC == "" && len(p.IMSI) >= 5 {
		p.MNC = p.IMSI[3:5]
	}
	return p
}

// Build builds a prepared session from a profile.
func Build(p Profile) *PreparedSession {
	p = Normalize(p)
	return &PreparedSession{
		Profile:  p,
		AuthPlan: AuthPlan{},
	}
}

// ResolveIdentityIMEI resolves the IMEI used for the identity.
func ResolveIdentityIMEI(p Profile, model string) string {
	if p.IMEI != "" {
		return p.IMEI
	}
	return GenerateStableIMEIForModel(model, p.IMSI)
}

// --- ISIM APDU reading ---

// APDU command APDUs (ISO 7816-4).
const (
	claISO7816 byte = 0x00
	insSelect  byte = 0xA4
	insReadBin byte = 0xB0
	insReadRec byte = 0xB2
	p1Select   byte = 0x04 // select by DF name
)

// EF identifiers for the ISIM application (3GPP TS 31.103).
const (
	efIMPI   uint16 = 0x6F02
	efDomain uint16 = 0x6F03
	efIMPU   uint16 = 0x6F04
)

// LogicalReader transmits APDUs to the SIM.
type LogicalReader interface {
	// Transmit sends an APDU and returns the response (SW1|SW2 appended).
	Transmit(apdu []byte) ([]byte, error)
}

// ReadISIMIdentity reads the ISIM identity (IMPI, IMPU, domain) from the SIM.
func ReadISIMIdentity(reader LogicalReader) (IMSIdentity, error) {
	if reader == nil {
		return IMSIdentity{}, errors.New("profile: no SIM reader")
	}
	// Select the ISIM application (AID 0xA0000000871002).
	selectAID := []byte{0x00, 0xA4, 0x04, 0x04, 0x07, 0xA0, 0x00, 0x00, 0x00, 0x87, 0x10, 0x02}
	if _, err := transmitLogicalWithFollowUp(reader, selectAID); err != nil {
		return IMSIdentity{}, fmt.Errorf("profile: select ISIM: %w", err)
	}

	impi, err := readTransparentEF(reader, efIMPI)
	if err != nil {
		return IMSIdentity{}, fmt.Errorf("profile: read IMPI: %w", err)
	}
	domain, err := readTransparentEF(reader, efDomain)
	if err != nil {
		return IMSIdentity{}, fmt.Errorf("profile: read domain: %w", err)
	}
	impuRecords, err := readLinearFixedRecords(reader, efIMPU)
	if err != nil {
		return IMSIdentity{}, fmt.Errorf("profile: read IMPU: %w", err)
	}

	identity := IMSIdentity{
		IMPI:   normalizeIdentityString(impi),
		Domain: normalizeIdentityString(domain),
	}
	for _, rec := range impuRecords {
		if v := normalizeIdentityString(rec); v != "" {
			identity.IMPU = appendUnique(identity.IMPU, v)
		}
	}
	return identity, nil
}

// transmitLogicalWithFollowUp transmits an APDU, following up on 61xx status
// words to read the remaining data.
func transmitLogicalWithFollowUp(reader LogicalReader, apdu []byte) ([]byte, error) {
	resp, err := reader.Transmit(apdu)
	if err != nil {
		return nil, err
	}
	return followUpIfNeededWithLimit(reader, resp, 0)
}

// followUpIfNeededWithLimit handles the 61xx (more data) status word.
func followUpIfNeededWithLimit(reader LogicalReader, resp []byte, limit int) ([]byte, error) {
	if len(resp) < 2 {
		return resp, nil
	}
	sw := resp[len(resp)-2:]
	body := resp[:len(resp)-2]
	if sw[0] == 0x61 {
		// More data available: read it with GET RESPONSE.
		le := sw[1]
		getResp := []byte{0x00, 0xC0, 0x00, 0x00, le}
		more, err := transmitLogicalWithFollowUp(reader, getResp)
		if err != nil {
			return nil, err
		}
		body = append(body, more...)
	}
	return body, nil
}

// readTransparentEF reads a transparent EF file.
func readTransparentEF(reader LogicalReader, efID uint16) ([]byte, error) {
	selectCmd := []byte{claISO7816, insSelect, 0x00, 0x00, 0x02, byte(efID >> 8), byte(efID)}
	fcp, err := transmitLogicalWithFollowUp(reader, selectCmd)
	if err != nil {
		return nil, err
	}
	size, err := parseTransparentFileSizeFromFCP(fcp)
	if err != nil {
		return nil, err
	}
	readCmd := []byte{claISO7816, insReadBin, 0x00, 0x00, byte(size)}
	return transmitLogicalWithFollowUp(reader, readCmd)
}

// readLinearFixedRecords reads all records of a linear-fixed EF file.
func readLinearFixedRecords(reader LogicalReader, efID uint16) ([][]byte, error) {
	selectCmd := []byte{claISO7816, insSelect, 0x00, 0x00, 0x02, byte(efID >> 8), byte(efID)}
	fcp, err := transmitLogicalWithFollowUp(reader, selectCmd)
	if err != nil {
		return nil, err
	}
	recLen, count, err := parseLinearFixedMetaFromFCP(fcp)
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for i := 1; i <= count; i++ {
		readCmd := []byte{claISO7816, insReadRec, byte(i), 0x04, byte(recLen)}
		rec, err := transmitLogicalWithFollowUp(reader, readCmd)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// parseTransparentFileSizeFromFCP extracts the file size from the FCP.
func parseTransparentFileSizeFromFCP(fcp []byte) (int, error) {
	// FCP: 62 len <TLVs> ... 80 len size(2) ...
	tlvs := collectTLVValues(fcp)
	// Unwrap the 0x62 (FCP template) container.
	if v, ok := tlvs[0x62]; ok {
		tlvs = collectTLVValues(v)
	}
	if v, ok := tlvs[0x80]; ok && len(v) >= 2 {
		return int(v[0])<<8 | int(v[1]), nil
	}
	return 0, errors.New("profile: FCP missing file size")
}

// parseLinearFixedMetaFromFCP extracts record length and count from the FCP.
func parseLinearFixedMetaFromFCP(fcp []byte) (recLen, count int, err error) {
	tlvs := collectTLVValues(fcp)
	if v, ok := tlvs[0x62]; ok {
		tlvs = collectTLVValues(v)
	}
	if v, ok := tlvs[0x80]; ok && len(v) >= 2 {
		recLen = int(v[0])<<8 | int(v[1])
	}
	if v, ok := tlvs[0x88]; ok && len(v) >= 1 {
		count = int(v[0])
	}
	if recLen == 0 || count == 0 {
		return 0, 0, errors.New("profile: FCP missing record metadata")
	}
	return recLen, count, nil
}

// collectTLVValues parses a TLV stream into a map keyed by tag.
func collectTLVValues(b []byte) map[byte][]byte {
	out := make(map[byte][]byte)
	for i := 0; i+1 < len(b); {
		tag := b[i]
		length := int(b[i+1])
		i += 2
		if i+length > len(b) {
			break
		}
		out[tag] = b[i : i+length]
		i += length
	}
	return out
}

// decodeIdentityValues decodes the identity TLV values (tag 0x80 = value).
func decodeIdentityValues(b []byte) []string {
	tlvs := collectTLVValues(b)
	var out []string
	for _, v := range tlvs {
		if s := normalizeIdentityString(v); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// normalizeIdentityString trims and validates an identity string.
func normalizeIdentityString(b []byte) string {
	s := strings.TrimSpace(string(b))
	s = strings.Trim(s, "\x00")
	return s
}

// appendUnique appends a value if not already present.
func appendUnique(list []string, v string) []string {
	for _, e := range list {
		if e == v {
			return list
		}
	}
	return append(list, v)
}

// transmitHex is a helper for hex-encoded APDU transmission.
func transmitHex(reader LogicalReader, hexAPDU string) ([]byte, error) {
	apdu, err := hex.DecodeString(strings.ReplaceAll(hexAPDU, " ", ""))
	if err != nil {
		return nil, err
	}
	return transmitLogicalWithFollowUp(reader, apdu)
}

// extractSuccessData extracts the data from a successful APDU response.
func extractSuccessData(resp []byte) ([]byte, error) {
	if len(resp) < 2 {
		return nil, errors.New("profile: short APDU response")
	}
	sw := resp[len(resp)-2:]
	if sw[0] != 0x90 || sw[1] != 0x00 {
		return nil, fmt.Errorf("profile: APDU status %02x%02x", sw[0], sw[1])
	}
	return resp[:len(resp)-2], nil
}

// --- model-based IMEI / user agent ---

// GenerateStableIMEIForModel generates a stable IMEI from a model name and
// IMSI (deterministic, Luhn-valid).
func GenerateStableIMEIForModel(model, imsi string) string {
	// TAC from the model, or a stable fallback.
	tac := resolveKnownModelTAC(model)
	if tac == "" {
		tac = "35693803"
	}
	// Serial from the IMSI (last 6 digits).
	serial := "000000"
	if len(imsi) >= 6 {
		serial = imsi[len(imsi)-6:]
	}
	prefix := tac + serial
	if len(prefix) > 14 {
		prefix = prefix[:14]
	}
	for len(prefix) < 14 {
		prefix += "0"
	}
	return prefix + string(imeiLuhnCheckDigit(prefix))
}

// imeiLuhnCheckDigit computes the Luhn check digit for a 14-digit prefix.
func imeiLuhnCheckDigit(prefix string) byte {
	sum := 0
	for i := 0; i < 14; i++ {
		d := int(prefix[i] - '0')
		if i%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return byte('0' + (10-sum%10)%10)
}

// resolveKnownModelTAC resolves a known model to its TAC.
func resolveKnownModelTAC(model string) string {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "iphone15,4":
		return "86034905"
	}
	switch normalizeModelName(model) {
	case "iphone":
		return "35693803"
	case "pixel":
		return "35929006"
	case "galaxy":
		return "35340610"
	default:
		return ""
	}
}

// normalizeModelName normalizes a model name for matching.
func normalizeModelName(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(m, "iphone"):
		return "iphone"
	case strings.Contains(m, "pixel"):
		return "pixel"
	case strings.Contains(m, "galaxy"):
		return "galaxy"
	default:
		return m
	}
}

// ResolveUserAgentForModel resolves the SIP User-Agent for a device model.
func ResolveUserAgentForModel(model string) string {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "iphone15,4":
		return "iOS/18.2.1 iPhone (iPhone15,4)"
	}
	switch normalizeModelName(model) {
	case "iphone":
		return "iOS/18.2.1 iPhone (iPhone15,4)"
	case "pixel":
		return "Android"
	case "galaxy":
		return "Samsung"
	case "rmx3366":
		return "realme_RMX3366_0.0.2100"
	default:
		return "iOS/18.2.1 iPhone (iPhone15,4)"
	}
}
