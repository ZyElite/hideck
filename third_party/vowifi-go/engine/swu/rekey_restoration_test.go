package swu

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
)

var (
	_ func(*Session, []byte, []byte, []byte, []byte, uint64, uint64) (*IKEKeys, error)                          = (*Session).GenerateIKESARekeyKeys
	_ func(*Session, uint32, []ikev2.Payload) error                                                             = (*Session).HandleRekeyIKESARequest
	_ func(*Session, []byte, []byte, *enginecrypto.DiffieHellman, uint64, []byte, uint64, uint64) error         = (*Session).handleRekeyIKESAResp
	_ func(*Session, []byte, []byte, uint32, *enginecrypto.DiffieHellman) error                                 = (*Session).handleCreateChildSAResp
	_ func(*Session, []uint32) error                                                                            = (*Session).sendDeleteChildSA
	_ func(*Session, time.Duration)                                                                             = (*Session).startIKESARekeyTimer
	_ func(*Session, time.Duration)                                                                             = (*Session).startChildSARekeyTimer
	_ func(*Session, uint32, uint32, *ipsec.SecurityAssociation, *ipsec.SecurityAssociation, uint16, int) error = (*Session).rekeyXFRMSA
)

func TestGenerateIKESARekeyKeysUsesExplicitInputsWithoutMutation(t *testing.T) {
	session := newKeyDerivationSession(t)
	active := &IKEKeys{SK_d: bytes.Repeat([]byte{0x99}, 20)}
	session.ikeKeys = active
	oldSKd := bytes.Repeat([]byte{0x31}, 20)
	shared := bytes.Repeat([]byte{0x44}, 128)
	ni, nr := bytes.Repeat([]byte{0x51}, 32), bytes.Repeat([]byte{0x52}, 32)
	const spiI, spiR = uint64(0x0102030405060708), uint64(0x1112131415161718)

	keys, err := session.GenerateIKESARekeyKeys(oldSKd, shared, ni, nr, spiI, spiR)
	if err != nil {
		t.Fatalf("GenerateIKESARekeyKeys: %v", err)
	}
	seed := append(append(append([]byte{}, shared...), ni...), nr...)
	wantSKEYSEED := session.prf.Compute(oldSKd, seed)
	if !bytes.Equal(keys.SKEYSEED, wantSKEYSEED) {
		t.Fatalf("SKEYSEED = %x, want %x", keys.SKEYSEED, wantSKEYSEED)
	}
	if session.ikeKeys != active || !bytes.Equal(active.SK_d, bytes.Repeat([]byte{0x99}, 20)) {
		t.Fatal("explicit rekey derivation mutated the active IKE SA")
	}
}

func TestBuildChildSARekeyPayloadsUsesRemoteSPIAndLegacyOrder(t *testing.T) {
	session := NewSession(&Config{})
	session.espRemoteSPI = 0x50607080
	tsi, tsr := buildTrafficSelectorsForIPStack([]byte{10, 0, 0, 2})
	payloads := session.buildChildSARekeyPayloads(childSARekeyRequest{
		localSPI: 0x10203040, nonce: bytes.Repeat([]byte{0x41}, 32), tsi: tsi, tsr: tsr,
	})
	wantTypes := []ikev2.PayloadType{ikev2.PayloadSA, ikev2.PayloadNi, ikev2.PayloadNotify, ikev2.PayloadTSi, ikev2.PayloadTSr}
	for index, want := range wantTypes {
		if payloads[index].Type() != want {
			t.Fatalf("payload %d type = %d, want %d", index, payloads[index].Type(), want)
		}
	}
	notify := payloads[2].(*ikev2.EncryptedPayloadNotify)
	if got := binary.BigEndian.Uint32(notify.SPI); got != session.espRemoteSPI {
		t.Fatalf("REKEY_SA SPI = %08x, want remote SPI %08x", got, session.espRemoteSPI)
	}
}

func TestPFSChildSARekeyPayloadsCarryFreshKE(t *testing.T) {
	session := NewSession(&Config{})
	session.espRemoteSPI = 0x50607080
	dh, err := enginecrypto.NewDiffieHellman(14)
	if err != nil || dh.GenerateKey() != nil {
		t.Fatalf("DH: %v", err)
	}
	tsi, tsr := buildTrafficSelectorsForIPStack([]byte{10, 0, 0, 2})
	payloads := session.buildChildSARekeyPayloads(childSARekeyRequest{
		localSPI: 0x10203040, nonce: bytes.Repeat([]byte{0x41}, 32), tsi: tsi, tsr: tsr, dh: dh,
	})
	wantTypes := []ikev2.PayloadType{
		ikev2.PayloadSA, ikev2.PayloadNi, ikev2.PayloadKE,
		ikev2.PayloadNotify, ikev2.PayloadTSi, ikev2.PayloadTSr,
	}
	for index, want := range wantTypes {
		if payloads[index].Type() != want {
			t.Fatalf("PFS payload %d type = %d, want %d", index, payloads[index].Type(), want)
		}
	}
	if got := proposalDHGroup(payloads[0].(*ikev2.EncryptedPayloadSA).Proposals[0]); got != 14 {
		t.Fatalf("PFS proposal DH = %d, want 14", got)
	}
}

func TestChildSARekeyRejectsUnexpectedAndDuplicateKE(t *testing.T) {
	dh, err := enginecrypto.NewDiffieHellman(14)
	if err != nil || dh.GenerateKey() != nil {
		t.Fatalf("DH: %v", err)
	}
	peer, err := enginecrypto.NewDiffieHellman(14)
	if err != nil || peer.GenerateKey() != nil {
		t.Fatalf("peer DH: %v", err)
	}
	ke := &ikev2.EncryptedPayloadKE{
		DHGroup: ikev2.AlgorithmType(14), KEData: peer.PublicKeyBytes(),
	}
	if _, err := childRekeySharedSecret([]ikev2.Payload{ke}, nil); err == nil {
		t.Fatal("unoffered KE should fail")
	}
	if _, err := childRekeySharedSecret([]ikev2.Payload{ke, ke}, dh); err == nil {
		t.Fatal("duplicate KE should fail")
	}
}

func TestIKEAuthInitialChildPFSBuildsProposalAndKE(t *testing.T) {
	session := NewSession(&Config{
		IMSI: "001010123456789", APN: "ims", ESPProposals: []string{"aes128-sha1-modp2048"},
	})
	payloads, err := session.buildIKEAuthInitPayloads()
	if err != nil {
		t.Fatalf("buildIKEAuthInitPayloads: %v", err)
	}
	var sa *ikev2.EncryptedPayloadSA
	var ke *ikev2.EncryptedPayloadKE
	for _, payload := range payloads {
		switch value := payload.(type) {
		case *ikev2.EncryptedPayloadSA:
			sa = value
		case *ikev2.EncryptedPayloadKE:
			ke = value
		}
	}
	if sa == nil || ke == nil || proposalDHGroup(sa.Proposals[0]) != 14 || childDHGroup(session.childDH) != 14 {
		t.Fatalf("initial PFS SA=%v KE=%v childDH=%v", sa != nil, ke != nil, session.childDH)
	}
}

func TestESPProposalRejectsMixedPFSModes(t *testing.T) {
	_, err := buildESPProposals(&Config{
		ESPProposals: []string{"aes128-sha1", "aes128-sha1-modp2048"},
	}, nil)
	if err == nil {
		t.Fatal("mixed PFS and non-PFS proposals should fail")
	}
}

func TestChildSARejectionPreservesNotifyType(t *testing.T) {
	payloads := []ikev2.Payload{&ikev2.EncryptedPayloadNotify{NotifyType: ikev2.CHILD_SA_NOT_FOUND}}
	err := childSARejectionError(payloads)
	if !isChildSANotFoundError(err) {
		t.Fatalf("error = %v, want typed CHILD_SA_NOT_FOUND", err)
	}
}

func TestIncomingRekeyCollisionReturnsTemporaryFailure(t *testing.T) {
	session, transport := newEstablishedControlSession(t)
	defer stopControlTestSession(session)
	session.rekeyMu.Lock()
	err := session.handleIncomingCreateChildSAParsed(77, nil)
	session.rekeyMu.Unlock()
	if err != nil {
		t.Fatalf("handleIncomingCreateChildSAParsed: %v", err)
	}
	raw := receiveFragmentPacket(t, transport.sentIKE)
	packet, err := ikev2.DecodePacket(raw)
	if err != nil {
		t.Fatalf("decode collision response: %v", err)
	}
	payloads, err := session.decryptAndParse(packet)
	if err != nil || len(payloads) != 1 {
		t.Fatalf("collision response payloads=%d err=%v", len(payloads), err)
	}
	notify, ok := payloads[0].(*ikev2.EncryptedPayloadNotify)
	if !ok || notify.NotifyType != ikev2.TEMPORARY_FAILURE {
		t.Fatalf("collision response = %#v", payloads[0])
	}
}

func TestValidateIKERekeyProposalAcceptsAEADWithoutIntegrity(t *testing.T) {
	session := &Session{
		encrAlg: enginecrypto.EncrAESGCM16, encKeyBits: 128, aead: true,
		prfAlg: 2, dhGroup: 14,
	}
	proposal := buildIKEProposalsForSession(session)[0]
	proposal.SPI = []byte{1, 2, 3, 4, 5, 6, 7, 8}
	if err := session.validateIKERekeyProposal(proposal); err != nil {
		t.Fatalf("validateIKERekeyProposal: %v", err)
	}
}

func TestIKEAuthLifetimeControlsRecoveredRekeyIntervals(t *testing.T) {
	session := NewSession(&Config{})
	notify := &ikev2.EncryptedPayloadNotify{
		NotifyType: ikev2.AUTH_LIFETIME, NotifyData: []byte{0, 0, 3, 0xe8},
	}
	if err := session.applyIKEAuthLifetime([]ikev2.Payload{notify}); err != nil {
		t.Fatalf("applyIKEAuthLifetime: %v", err)
	}
	ikeInterval, childInterval := session.rekeyIntervals()
	if ikeInterval != 800*time.Second || childInterval != 905*time.Second {
		t.Fatalf("intervals = %s/%s, want 800s/905s", ikeInterval, childInterval)
	}
	bad := &ikev2.EncryptedPayloadNotify{NotifyType: ikev2.AUTH_LIFETIME, NotifyData: []byte{1, 2, 3}}
	if err := session.applyIKEAuthLifetime([]ikev2.Payload{bad}); err == nil {
		t.Fatal("short AUTH_LIFETIME should fail")
	}
	extended := &ikev2.EncryptedPayloadNotify{
		NotifyType: ikev2.AUTH_LIFETIME, NotifyData: []byte{0, 0, 0, 10, 0xff},
	}
	if err := session.applyIKEAuthLifetime([]ikev2.Payload{extended}); err != nil || session.authLifetime != 10 {
		t.Fatalf("extended AUTH_LIFETIME = %d, err=%v", session.authLifetime, err)
	}
}

func TestRecoveredRekeyIntervalDefaultsAndOverrides(t *testing.T) {
	session := NewSession(&Config{})
	ikeInterval, childInterval := session.rekeyIntervals()
	if ikeInterval != defaultIKERekeyInterval || childInterval != defaultChildRekeyInterval+childRekeyStartOffset {
		t.Fatalf("defaults = %s/%s", ikeInterval, childInterval)
	}
	session.cfg.RekeyIKESeconds = 7 * time.Minute
	session.cfg.RekeyChildSeconds = 11 * time.Minute
	ikeInterval, childInterval = session.rekeyIntervals()
	if ikeInterval != 7*time.Minute || childInterval != 11*time.Minute {
		t.Fatalf("overrides = %s/%s", ikeInterval, childInterval)
	}
}

func TestRecoveredRekeyCooldownsAreIndependent(t *testing.T) {
	session := NewSession(&Config{})
	session.lastIKERekeyTime = time.Now()
	if !session.ikeRekeyInCooldown() || session.childRekeyInCooldown() {
		t.Fatal("IKE rekey cooldown leaked into CHILD_SA cooldown")
	}
	session.lastRekeyTime = time.Now()
	if !session.childRekeyInCooldown() {
		t.Fatal("CHILD_SA rekey cooldown was not recorded")
	}
}

func TestSendDeleteChildSARejectsUnencodableCount(t *testing.T) {
	session := NewSession(&Config{})
	err := session.sendDeleteChildSA(make([]uint32, int(^uint16(0))+1))
	if err == nil {
		t.Fatal("unencodable Delete SPI count should fail")
	}
}

type rejectingKernelDataPlane struct {
	err        error
	retiredSPI uint32
}

func (*rejectingKernelDataPlane) Close() error             { return nil }
func (*rejectingKernelDataPlane) DeviceName() string       { return "test-xfrm" }
func (*rejectingKernelDataPlane) EnsureIPv6Enabled() error { return nil }
func (plane *rejectingKernelDataPlane) Rekey(*Session, *childSARuntime) error {
	return plane.err
}
func (plane *rejectingKernelDataPlane) RetireInbound(spi uint32) error {
	plane.retiredSPI = spi
	return plane.err
}

func TestKernelRekeyFailureDoesNotSwitchSessionRuntime(t *testing.T) {
	session := NewSession(&Config{})
	session.espLocalSPI, session.espRemoteSPI = 1, 2
	sentinel := errors.New("kernel transaction failed")
	session.kernelDataPlane = &rejectingKernelDataPlane{err: sentinel}
	err := session.activateChildSARuntime(&childSARuntime{localSPI: 3, remoteSPI: 4})
	if !errors.Is(err, sentinel) {
		t.Fatalf("activateChildSARuntime error = %v", err)
	}
	if session.espLocalSPI != 1 || session.espRemoteSPI != 2 {
		t.Fatal("kernel failure switched in-memory CHILD_SA")
	}
}

func TestRetiringChildSACleansKernelOverlap(t *testing.T) {
	session := NewSession(&Config{})
	plane := &rejectingKernelDataPlane{}
	session.kernelDataPlane = plane
	session.espInboundSAs[3] = &ipsec.SecurityAssociation{SPI: 3}
	session.retiredChildSAs[3] = 4
	session.retireInboundChildSA(3)
	if plane.retiredSPI != 3 {
		t.Fatalf("retired kernel SPI = %d, want 3", plane.retiredSPI)
	}
}
