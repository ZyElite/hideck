package swu

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
)

const ikev2KeyPad = "Key Pad for IKEv2"

func (s *Session) computeEAPInitiatorAuth() (*ikev2.EncryptedPayloadAuth, error) {
	if s.ikeKeys == nil || s.prf == nil {
		return nil, errors.New("swu: no IKE SA keys for AUTH")
	}
	if len(s.eapKeys.MSK) == 0 {
		return nil, errors.New("swu: no EAP MSK for initiator AUTH")
	}
	idType, idData := s.currentIKEIdentity()
	signed, err := s.initiatorSignedOctets(idType, idData)
	if err != nil {
		return nil, err
	}
	sharedKey := s.prf.Compute(s.eapKeys.MSK, []byte(ikev2KeyPad))
	auth := s.prf.Compute(sharedKey, signed)
	return &ikev2.EncryptedPayloadAuth{AuthMethod: ikev2.AuthMethodSharedKey, AuthData: auth}, nil
}

func (s *Session) initiatorSignedOctets(idType byte, idData []byte) ([]byte, error) {
	if len(s.ikeSAInitRequest) == 0 || len(s.nr) == 0 {
		return nil, errors.New("swu: incomplete IKE_SA_INIT transcript for initiator AUTH")
	}
	macedID := s.prf.Compute(s.ikeKeys.SK_pi, identityPayloadBody(idType, idData))
	out := append([]byte(nil), s.ikeSAInitRequest...)
	out = append(out, s.nr...)
	return append(out, macedID...), nil
}

func (s *Session) responderSignedOctets(idType byte, idData []byte) ([]byte, error) {
	if len(s.ikeSAInitResponse) == 0 || len(s.Ni) == 0 {
		return nil, errors.New("swu: incomplete IKE_SA_INIT transcript for responder AUTH")
	}
	macedID := s.prf.Compute(s.ikeKeys.SK_pr, identityPayloadBody(idType, idData))
	out := append([]byte(nil), s.ikeSAInitResponse...)
	out = append(out, s.Ni...)
	return append(out, macedID...), nil
}

func identityPayloadBody(idType byte, idData []byte) []byte {
	body := []byte{idType, 0, 0, 0}
	return append(body, idData...)
}

func (s *Session) verifyResponderCertificateAuth(payloads []ikev2.Payload) error {
	idType, idData, auth, certs, err := responderAuthMaterial(payloads)
	if err != nil {
		return err
	}
	signed, err := s.responderSignedOctets(idType, idData)
	if err != nil {
		return err
	}
	leaf, err := verifyResponderCertificate(certs, idType, idData)
	if err != nil {
		return err
	}
	switch auth.AuthMethod {
	case ikev2.AuthMethodRSA:
		return verifyLegacyRSASignature(leaf, signed, auth.AuthData)
	case ikev2.AuthMethodDigitalSignature:
		return verifyGenericSignature(leaf, signed, auth.AuthData)
	default:
		return fmt.Errorf("swu: unsupported responder AUTH method %d", auth.AuthMethod)
	}
}

func (s *Session) authenticateInitialResponder(payloads []ikev2.Payload) (bool, error) {
	if err := ikeAuthenticationError(payloads); err != nil {
		return false, err
	}
	idType, idData, hasID := responderIdentity(payloads)
	if hasPayloadType(payloads, ikev2.PayloadAuth) {
		if err := s.verifyResponderCertificateAuth(payloads); err != nil {
			return false, err
		}
		s.responderIDType = idType
		s.responderID = append([]byte(nil), idData...)
		return false, nil
	}
	if !s.eapOnlyRequested {
		return false, errors.New("swu: responder omitted AUTH without negotiated EAP-only authentication")
	}
	if hasPayloadType(payloads, ikev2.PayloadCert) {
		return false, errors.New("swu: EAP-only response included a certificate without AUTH")
	}
	if !hasPayloadType(payloads, ikev2.PayloadEAP) {
		return false, fmt.Errorf(
			"swu: IKE_AUTH response missing responder authentication material (payloads=%s)",
			ikePayloadTypes(payloads),
		)
	}
	// 3GPP overloads the initiator's IDr with the target APN. If an ePDG
	// omits its own IDr in the EAP-only response, bind the final MSK AUTH to
	// the configured endpoint identity instead of treating the APN as IDr.
	if !hasID {
		idType, idData, hasID = configuredEPDGIdentity(s.cfg.EPDGAddr)
	}
	if !hasID {
		return false, fmt.Errorf(
			"swu: EAP-only response omitted IDr without a configured ePDG identity (payloads=%s)",
			ikePayloadTypes(payloads),
		)
	}
	s.eapOnlyAuthentication = true
	s.responderIDType = idType
	s.responderID = append([]byte(nil), idData...)
	return true, nil
}

func configuredEPDGIdentity(address string) (byte, []byte, bool) {
	host := strings.TrimSpace(address)
	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		host = splitHost
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return 0, nil, false
	}
	if ip := net.ParseIP(host); ip != nil {
		if ipv4 := ip.To4(); ipv4 != nil {
			return ikev2.IDTypeIPv4Address, append([]byte(nil), ipv4...), true
		}
		return ikev2.IDTypeIPv6Address, append([]byte(nil), ip.To16()...), true
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return ikev2.IDTypeFQDN, []byte(host), true
}

func (s *Session) verifyEAPResponderAuth(payloads []ikev2.Payload) error {
	if s.eapType != eapaka.TypeAKA && s.eapType != eapaka.TypeAKAPrime {
		return fmt.Errorf("swu: responder AUTH used unsafe EAP type %d", s.eapType)
	}
	if len(s.eapKeys.MSK) == 0 || len(s.responderID) == 0 {
		return errors.New("swu: incomplete EAP responder authentication state")
	}
	auth := responderAuthPayload(payloads)
	if auth == nil || auth.AuthMethod != ikev2.AuthMethodSharedKey || len(auth.AuthData) == 0 {
		return fmt.Errorf("swu: final EAP IKE_AUTH response missing MSK AUTH (payloads=%s)", ikePayloadTypes(payloads))
	}
	signed, err := s.responderSignedOctets(s.responderIDType, s.responderID)
	if err != nil {
		return err
	}
	sharedKey := s.prf.Compute(s.eapKeys.MSK, []byte(ikev2KeyPad))
	expected := s.prf.Compute(sharedKey, signed)
	if !hmac.Equal(auth.AuthData, expected) {
		return errors.New("swu: EAP responder MSK AUTH verification failed")
	}
	return nil
}

func responderIdentity(payloads []ikev2.Payload) (byte, []byte, bool) {
	for _, payload := range payloads {
		if payload == nil || payload.Type() != ikev2.PayloadIDr {
			continue
		}
		identity, ok := payload.(*ikev2.EncryptedPayloadID)
		if ok && len(identity.IDData) > 0 {
			return identity.IDType, append([]byte(nil), identity.IDData...), true
		}
	}
	return 0, nil, false
}

func responderAuthPayload(payloads []ikev2.Payload) *ikev2.EncryptedPayloadAuth {
	for _, payload := range payloads {
		if payload != nil && payload.Type() == ikev2.PayloadAuth {
			auth, _ := payload.(*ikev2.EncryptedPayloadAuth)
			return auth
		}
	}
	return nil
}

func hasPayloadType(payloads []ikev2.Payload, payloadType ikev2.PayloadType) bool {
	for _, payload := range payloads {
		if payload != nil && payload.Type() == payloadType {
			return true
		}
	}
	return false
}

func ikeAuthenticationError(payloads []ikev2.Payload) error {
	for _, payload := range payloads {
		if payload == nil || payload.Type() != ikev2.PayloadNotify {
			continue
		}
		notifyType, _, ok := parseNotifyPayload(payload)
		if !ok {
			return errors.New("swu: malformed IKE_AUTH Notify payload")
		}
		if notifyType < 16384 {
			return fmt.Errorf("swu: IKE_AUTH rejected with %s (%d)", ikev2.NotifyTypeToString(notifyType), notifyType)
		}
	}
	return nil
}

func ikePayloadTypes(payloads []ikev2.Payload) string {
	types := make([]string, 0, len(payloads))
	for _, payload := range payloads {
		if payload == nil {
			types = append(types, "nil")
			continue
		}
		types = append(types, fmt.Sprintf("%d", payload.Type()))
	}
	return strings.Join(types, ",")
}

func responderAuthMaterial(payloads []ikev2.Payload) (byte, []byte, *ikev2.EncryptedPayloadAuth, []*x509.Certificate, error) {
	var idType byte
	var idData []byte
	var auth *ikev2.EncryptedPayloadAuth
	var certs []*x509.Certificate
	for _, payload := range payloads {
		switch payload.Type() {
		case ikev2.PayloadIDr:
			if id, ok := payload.(*ikev2.EncryptedPayloadID); ok {
				idType, idData = id.IDType, append([]byte(nil), id.IDData...)
			}
		case ikev2.PayloadAuth:
			auth, _ = payload.(*ikev2.EncryptedPayloadAuth)
		case ikev2.PayloadCert:
			certificate, err := parseX509CertificatePayload(payload)
			if err != nil {
				return 0, nil, nil, nil, err
			}
			certs = append(certs, certificate)
		}
	}
	if len(idData) == 0 || auth == nil || len(auth.AuthData) == 0 {
		return 0, nil, nil, nil, errors.New("swu: IKE_AUTH response missing IDr or AUTH")
	}
	if len(certs) == 0 {
		return 0, nil, nil, nil, errors.New("swu: certificate responder AUTH has no X.509 certificate")
	}
	return idType, idData, auth, certs, nil
}

func parseX509CertificatePayload(payload ikev2.Payload) (*x509.Certificate, error) {
	raw, ok := payload.(*ikev2.RawPayload)
	if !ok || len(raw.Data) < 2 {
		return nil, errors.New("swu: malformed CERT payload")
	}
	if raw.Data[0] != 4 {
		return nil, fmt.Errorf("swu: unsupported CERT encoding %d", raw.Data[0])
	}
	certificate, err := x509.ParseCertificate(raw.Data[1:])
	if err != nil {
		return nil, fmt.Errorf("swu: parse responder certificate: %w", err)
	}
	return certificate, nil
}

func verifyResponderCertificate(certs []*x509.Certificate, idType byte, idData []byte) (*x509.Certificate, error) {
	leaf := certs[0]
	intermediates := x509.NewCertPool()
	for _, certificate := range certs[1:] {
		intermediates.AddCert(certificate)
	}
	options := x509.VerifyOptions{Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}
	if idType == 2 {
		options.DNSName = strings.TrimSpace(string(idData))
	}
	if _, err := leaf.Verify(options); err != nil {
		return nil, fmt.Errorf("swu: verify responder certificate: %w", err)
	}
	return leaf, nil
}

func verifyLegacyRSASignature(certificate *x509.Certificate, signed, signature []byte) error {
	publicKey, ok := certificate.PublicKey.(*rsa.PublicKey)
	if !ok {
		return errors.New("swu: RSA AUTH certificate does not contain an RSA key")
	}
	digest := sha1.Sum(signed)
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA1, digest[:], signature); err != nil {
		return fmt.Errorf("swu: responder RSA AUTH verification failed: %w", err)
	}
	return nil
}

func verifyGenericSignature(certificate *x509.Certificate, signed, authData []byte) error {
	if len(authData) < 2 {
		return errors.New("swu: digital-signature AUTH is truncated")
	}
	algorithmLength := int(authData[0])
	if algorithmLength == 0 || 1+algorithmLength >= len(authData) {
		return errors.New("swu: invalid AUTH AlgorithmIdentifier length")
	}
	var algorithm pkix.AlgorithmIdentifier
	if rest, err := asn1.Unmarshal(authData[1:1+algorithmLength], &algorithm); err != nil || len(rest) != 0 {
		return errors.New("swu: invalid AUTH AlgorithmIdentifier")
	}
	hash, pss, err := signatureHash(algorithm.Algorithm)
	if err != nil {
		return err
	}
	digest, err := hashMessage(hash, signed)
	if err != nil {
		return err
	}
	signature := authData[1+algorithmLength:]
	return verifyPublicKeySignature(certificate.PublicKey, hash, digest, signature, pss)
}

func signatureHash(oid asn1.ObjectIdentifier) (crypto.Hash, bool, error) {
	switch oid.String() {
	case "1.2.840.113549.1.1.5":
		return crypto.SHA1, false, nil
	case "1.2.840.113549.1.1.11", "1.2.840.10045.4.3.2":
		return crypto.SHA256, false, nil
	case "1.2.840.113549.1.1.12", "1.2.840.10045.4.3.3":
		return crypto.SHA384, false, nil
	case "1.2.840.113549.1.1.13", "1.2.840.10045.4.3.4":
		return crypto.SHA512, false, nil
	case "1.2.840.113549.1.1.10":
		return crypto.SHA256, true, nil
	default:
		return 0, false, fmt.Errorf("swu: unsupported AUTH signature algorithm %s", oid)
	}
}

func hashMessage(hash crypto.Hash, message []byte) ([]byte, error) {
	switch hash {
	case crypto.SHA1:
		sum := sha1.Sum(message)
		return sum[:], nil
	case crypto.SHA256:
		sum := sha256.Sum256(message)
		return sum[:], nil
	case crypto.SHA384:
		sum := sha512.Sum384(message)
		return sum[:], nil
	case crypto.SHA512:
		sum := sha512.Sum512(message)
		return sum[:], nil
	default:
		return nil, errors.New("swu: unsupported AUTH hash")
	}
}

func verifyPublicKeySignature(publicKey any, hash crypto.Hash, digest, signature []byte, pss bool) error {
	switch key := publicKey.(type) {
	case *rsa.PublicKey:
		if pss {
			return rsa.VerifyPSS(key, hash, digest, signature, nil)
		}
		return rsa.VerifyPKCS1v15(key, hash, digest, signature)
	case *ecdsa.PublicKey:
		if !ecdsa.VerifyASN1(key, digest, signature) {
			return errors.New("swu: responder ECDSA AUTH verification failed")
		}
		return nil
	default:
		return fmt.Errorf("swu: unsupported responder public key %T", publicKey)
	}
}
