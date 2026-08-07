package imscore

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// DigestChallenge is a parsed WWW-Authenticate / Proxy-Authenticate challenge
// (RFC 2617).
type DigestChallenge struct {
	Realm     string
	Nonce     string
	Opaque    string
	Algorithm string
	QOP       string
	AKA       bool
	RAND      []byte // decoded RAND from the AKA nonce
	AUTN      []byte // decoded AUTN from the AKA nonce
}

// ParseDigestChallenge parses a WWW-Authenticate header value.
func ParseDigestChallenge(header string) (*DigestChallenge, error) {
	header = strings.TrimSpace(header)
	// Strip the leading scheme.
	if i := strings.IndexByte(header, ' '); i >= 0 {
		header = header[i+1:]
	}
	c := &DigestChallenge{}
	for _, param := range splitDigestParams(header) {
		key, value, ok := strings.Cut(param, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.Trim(strings.TrimSpace(value), "\"")
		switch key {
		case "realm":
			c.Realm = value
		case "nonce":
			c.Nonce = value
		case "opaque":
			c.Opaque = value
		case "algorithm":
			c.Algorithm = value
			c.AKA = strings.Contains(strings.ToUpper(value), "AKA")
		case "qop":
			c.QOP = value
		}
	}
	if c.Nonce == "" {
		return nil, errors.New("imscore: digest challenge missing nonce")
	}
	if c.AKA {
		if err := parseAKANonce(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// splitDigestParams splits a digest parameter list on commas, respecting
// quotes.
func splitDigestParams(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch r {
		case '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case ',':
			if inQuote {
				cur.WriteRune(r)
			} else {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// parseAKANonce decodes the RAND/AUTN from an AKA nonce: the nonce is
// base64(RAND | AUTN) (RFC 3310 §3.1).
func parseAKANonce(c *DigestChallenge) error {
	raw, err := base64.StdEncoding.DecodeString(c.Nonce)
	if err != nil {
		// Try URL-safe base64.
		raw, err = base64.RawURLEncoding.DecodeString(strings.TrimRight(c.Nonce, "="))
		if err != nil {
			return fmt.Errorf("imscore: decode AKA nonce: %w", err)
		}
	}
	if len(raw) < 32 {
		return errors.New("imscore: AKA nonce too short")
	}
	c.RAND = raw[:16]
	c.AUTN = raw[16:32]
	return nil
}

// ParseAKANonce decodes a raw AKA nonce into RAND and AUTN.
func ParseAKANonce(nonce string) (randBytes, autnBytes []byte, err error) {
	c := &DigestChallenge{Nonce: nonce, AKA: true}
	if err := parseAKANonce(c); err != nil {
		return nil, nil, err
	}
	return c.RAND, c.AUTN, nil
}

// md5Hex returns the hex MD5 digest of b.
func md5Hex(b []byte) string {
	h := md5.Sum(b)
	return hex.EncodeToString(h[:])
}

// ComputeAKAv1MD5DigestResponse computes the Digest-AKA response (RFC 3310):
//
//	response = H(H(A1):nonce:nc:cnonce:qop:H(A2))
//	A1 = username:realm:password   (password = the AKA RES hex)
//	A2 = method:uri
func ComputeAKAv1MD5DigestResponse(username, realm string, res []byte, method, uri, nonce, nc, cnonce, qop string) (string, error) {
	a1 := fmt.Sprintf("%s:%s:%s", username, realm, hex.EncodeToString(res))
	a2 := fmt.Sprintf("%s:%s", method, uri)
	ha1 := md5Hex([]byte(a1))
	ha2 := md5Hex([]byte(a2))
	response := md5Hex([]byte(fmt.Sprintf("%s:%s:%s:%s:%s:%s", ha1, nonce, nc, cnonce, qop, ha2)))
	return response, nil
}

// normalizeDigestQOP normalizes the qop value.
func normalizeDigestQOP(qop string) string {
	switch strings.ToLower(strings.TrimSpace(qop)) {
	case "auth":
		return "auth"
	case "auth-int":
		return "auth-int"
	default:
		return "auth"
	}
}

// randomDigestCNonce generates a client nonce.
func randomDigestCNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ProcessAKAChallenge handles a full AKA challenge: it computes the AKA
// (RES, CK, IK) and builds the Authorization response.
func ProcessAKAChallenge(challenge *DigestChallenge, aka AKAProvider, username, method, uri string) (string, error) {
	authorization, _, err := ProcessAKAChallengeWithResult(challenge, aka, username, method, uri)
	return authorization, err
}

// ProcessAKAChallengeWithResult also returns CK/IK for IMS IPsec setup.
func ProcessAKAChallengeWithResult(challenge *DigestChallenge, aka AKAProvider, username, method, uri string) (string, AKAResult, error) {
	if challenge == nil {
		return "", AKAResult{}, errors.New("imscore: nil challenge")
	}
	if !challenge.AKA {
		return "", AKAResult{}, errors.New("imscore: challenge is not AKA")
	}
	if aka == nil {
		return "", AKAResult{}, errors.New("imscore: no AKA provider")
	}
	result, err := aka.CalculateAKA(challenge.RAND, challenge.AUTN)
	if err != nil {
		return "", AKAResult{}, err
	}
	nc := "00000001"
	cnonce := randomDigestCNonce()
	qop := normalizeDigestQOP(challenge.QOP)
	response, err := ComputeAKAv1MD5DigestResponse(
		username, challenge.Realm, result.RES,
		method, uri, challenge.Nonce, nc, cnonce, qop,
	)
	if err != nil {
		return "", AKAResult{}, err
	}
	var b strings.Builder
	b.WriteString("Digest ")
	b.WriteString(fmt.Sprintf("username=\"%s\", realm=\"%s\", nonce=\"%s\", uri=\"%s\", ", username, challenge.Realm, challenge.Nonce, uri))
	b.WriteString(fmt.Sprintf("response=\"%s\", qop=%s, nc=%s, cnonce=\"%s\"", response, qop, nc, cnonce))
	if challenge.Algorithm != "" {
		b.WriteString(fmt.Sprintf(", algorithm=%s", challenge.Algorithm))
	}
	if challenge.Opaque != "" {
		b.WriteString(fmt.Sprintf(", opaque=\"%s\"", challenge.Opaque))
	}
	return b.String(), result, nil
}
