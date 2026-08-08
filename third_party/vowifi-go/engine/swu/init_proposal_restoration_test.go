package swu

import (
	"bytes"
	"errors"
	"testing"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func TestLegacyDefaultProposalSetsAndProfiles(t *testing.T) {
	ikeProposals, profiles, algorithms, err := buildIKEProposals(&Config{}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ikeProposals) != 4 || len(profiles) != 4 {
		t.Fatalf("IKE proposals/profiles = %d/%d, want 4/4", len(ikeProposals), len(profiles))
	}
	if profiles[0] != "p1:aes_cbc-hmac_sha2_256_128-hmac_sha2_256-modp_2048" {
		t.Fatalf("first profile = %q", profiles[0])
	}
	if len(algorithms) != 5 {
		t.Fatalf("effective algorithms = %v", algorithms)
	}
	espProposals, err := buildESPProposals(&Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(espProposals) != 4 {
		t.Fatalf("ESP proposals = %d, want 4", len(espProposals))
	}
}

func TestConfiguredProposalOrderAndProfileOffset(t *testing.T) {
	cfg := &Config{IKEProposals: []string{
		"aes256-sha512-prfsha512-modp2048",
		"aes128-sha1-prfsha1-modp1024",
	}}
	proposals, profiles, _, err := buildIKEProposals(cfg, []byte{1, 2}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(proposals) != 1 || len(profiles) != 1 || proposals[0].ProposalNum != 1 {
		t.Fatalf("offset proposals/profiles = %d/%d: %+v", len(proposals), len(profiles), proposals)
	}
	selection, err := firstIKEAlgorithmSelection(proposals[0])
	if err != nil {
		t.Fatal(err)
	}
	if selection.encryption != uint16(ikev2.ENCR_AES_CBC) || selection.keyBits != 128 ||
		selection.prf != uint16(ikev2.PRF_HMAC_SHA1) || selection.dh != uint16(ikev2.MODP_1024_bit) {
		t.Fatalf("selection = %+v", selection)
	}
}

func TestLegacyProposalRequiresExplicitPolicyPermission(t *testing.T) {
	configured := []string{"3des-sha1-modp1024"}
	_, _, _, err := buildIKEProposals(&Config{IKEProposals: configured}, nil, 0)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("rejected by policy")) {
		t.Fatalf("disabled legacy error = %v", err)
	}
	allowed := &Config{
		IKEProposals: configured, EnableLegacyCiphers: true,
		AllowedLegacyCiphers: []string{"triple_des"},
	}
	proposals, _, _, err := buildIKEProposals(allowed, nil, 0)
	if err != nil || len(proposals) != 1 {
		t.Fatalf("enabled legacy proposals = %d, error = %v", len(proposals), err)
	}
	strict := *allowed
	strict.AlgorithmPolicy = AlgorithmPolicyStrict
	if _, _, _, err := buildIKEProposals(&strict, nil, 0); err == nil {
		t.Fatal("strict policy accepted a legacy proposal")
	}
}

func TestIKEInitCookieRetainsOriginalExchangeMaterial(t *testing.T) {
	session := newInitSession(t)
	firstRaw, err := session.buildIKESAInitPacket()
	if err != nil {
		t.Fatal(err)
	}
	first, err := ikev2.DecodePacket(firstRaw)
	if err != nil {
		t.Fatal(err)
	}
	firstSPI, firstNonce := session.SPIi, append([]byte(nil), session.Ni...)
	firstPublic := append([]byte(nil), session.dh.PublicKeyBytes()...)
	if err := session.handleCookie([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	retryRaw, err := session.buildIKESAInitPacket()
	if err != nil {
		t.Fatal(err)
	}
	retry, err := ikev2.DecodePacket(retryRaw)
	if err != nil {
		t.Fatal(err)
	}
	if session.SPIi != firstSPI || !bytes.Equal(session.Ni, firstNonce) ||
		!bytes.Equal(session.dh.PublicKeyBytes(), firstPublic) {
		t.Fatal("COOKIE retry regenerated exchange material")
	}
	if len(retry.Payloads) != len(first.Payloads)+1 {
		t.Fatalf("COOKIE retry payload count = %d, first = %d", len(retry.Payloads), len(first.Payloads))
	}
	notify, ok := retry.Payloads[0].(*ikev2.EncryptedPayloadNotify)
	if !ok || notify.NotifyType != notifyCookie || !bytes.Equal(notify.NotifyData, []byte{1, 2, 3}) {
		t.Fatalf("first retry payload = %#v", retry.Payloads[0])
	}
}

func TestIKEInitNoProposalIsRetryableAndAdvancesProfile(t *testing.T) {
	session := newInitSession(t)
	session.cfg.IKEProposals = []string{
		"aes128-sha256-prfsha256-modp2048",
		"aes128-sha1-prfsha1-modp2048",
	}
	if _, err := session.buildIKESAInitPacket(); err != nil {
		t.Fatal(err)
	}
	errorPacket := &ikev2.IKEPacket{
		Header:   newIKEHeader(session.SPIi, [8]byte{1}, ikev2.IKE_SA_INIT, ikeResponseFlag, 0),
		Payloads: []ikev2.Payload{&ikev2.EncryptedPayloadNotify{NotifyType: uint16(ikev2.NO_PROPOSAL_CHOSEN)}},
	}
	err := session.handleIKESAInitResp(encodeInitPacket(t, errorPacket))
	var negotiationError *NegotiationError
	if !errors.As(err, &negotiationError) || !negotiationError.Retryable {
		t.Fatalf("NO_PROPOSAL_CHOSEN error = %v", err)
	}
	if !session.advanceIKEProfileOffset() || session.ikeProfileOffset != 1 {
		t.Fatal("profile offset did not advance")
	}
	next, _, _, err := buildIKEProposals(session.cfg, nil, session.ikeProfileOffset)
	if err != nil || len(next) != 1 {
		t.Fatalf("fallback proposals = %d, error = %v", len(next), err)
	}
}

func TestIKEInitAppliesASelectedFallbackProposal(t *testing.T) {
	session := NewSession(&Config{})
	if _, err := session.buildIKESAInitPacket(); err != nil {
		t.Fatal(err)
	}
	responderDH, err := newResponderDH(session.dhGroup)
	if err != nil {
		t.Fatal(err)
	}
	response := buildInitResp(t, session, responderDH)
	selected := ikev2.NewProposal(3, ikev2.ProtoIKE, nil)
	selected.AddTransformWithKeyLen(ikev2.TransformTypeEncr, ikev2.ENCR_AES_CBC, 128)
	selected.AddTransform(ikev2.TransformTypeInteg, ikev2.AUTH_HMAC_SHA1_96)
	selected.AddTransform(ikev2.TransformTypePRF, ikev2.PRF_HMAC_SHA1)
	selected.AddTransform(ikev2.TransformTypeDH, ikev2.AlgorithmType(session.dhGroup))
	response.Payloads[0] = &ikev2.EncryptedPayloadSA{Proposals: []*ikev2.Proposal{selected}}
	if err := session.handleIKESAInitResp(encodeInitPacket(t, response)); err != nil {
		t.Fatal(err)
	}
	if session.encrAlg != uint16(ikev2.ENCR_AES_CBC) ||
		session.integAlg != uint16(ikev2.AUTH_HMAC_SHA1_96) ||
		session.prfAlg != uint16(ikev2.PRF_HMAC_SHA1) {
		t.Fatalf("selected algorithms = %d/%d/%d", session.encrAlg, session.integAlg, session.prfAlg)
	}
}

func newResponderDH(group uint16) (*crypto.DiffieHellman, error) {
	return crypto.NewDiffieHellman(group)
}
