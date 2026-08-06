package swu

import (
	"bytes"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

// buildInitResp constructs a synthetic IKE_SA_INIT response from a "responder"
// DH instance, encodes and re-decodes it so the payloads reach the handler as
// RawPayloads (as they would over the wire).
func buildInitResp(t *testing.T, initiator *Session, responderDH *crypto.DiffieHellman, extraPayloads ...ikev2.Payload) *ikev2.IKEPacket {
	t.Helper()
	if err := responderDH.GenerateKey(); err != nil {
		t.Fatalf("responder GenerateKey: %v", err)
	}
	nr := make([]byte, 32)
	rand.Read(nr)
	var spir [8]byte
	rand.Read(spir[:])

	pkt := &ikev2.IKEPacket{
		InitiatorSPI: initiator.SPIi,
		ResponderSPI: spir,
		Version:      0x20,
		ExchangeType: ikev2.ExchangeIKEInit,
		Flags:        0x20, // Responder
		MessageID:    0,
		Payloads: append([]ikev2.Payload{
			&ikev2.EncryptedPayloadSA{Proposals: buildIKEProposals(initiator.encrAlg, initiator.prfAlg, initiator.integAlg, initiator.dhGroup)},
			&ikev2.EncryptedPayloadKE{DHGroupNum: initiator.dhGroup, KeyData: responderDH.PublicKeyBytes()},
			&ikev2.EncryptedPayloadNonce{Data: nr},
		}, extraPayloads...),
	}
	dec, err := ikev2.DecodePacket(pkt.Encode())
	if err != nil {
		t.Fatalf("DecodePacket(response): %v", err)
	}
	return dec
}

func TestHandleIKESAInitRespDerivesKeys(t *testing.T) {
	init := newInitSession(t)
	if _, err := init.buildIKESAInitPacket(); err != nil {
		t.Fatalf("buildIKESAInitPacket: %v", err)
	}

	respDH, err := crypto.NewDiffieHellman(14)
	if err != nil {
		t.Fatalf("responder DH: %v", err)
	}
	resp := buildInitResp(t, init, respDH)

	if err := init.handleIKESAInitResp(resp); err != nil {
		t.Fatalf("handleIKESAInitResp: %v", err)
	}
	if init.SPIr == ([8]byte{}) {
		t.Error("SPIr not recorded")
	}
	if init.ikeKeys == nil {
		t.Fatal("IKE keys not derived")
	}

	// The initiator's shared secret must match what the responder computes
	// from the initiator's public key.
	responderShared, err := respDH.ComputeSharedSecret(init.dh.PublicKeyBytes())
	if err != nil {
		t.Fatalf("responder shared: %v", err)
	}
	if !bytes.Equal(init.dhSharedSecret, responderShared) {
		t.Error("DH shared secrets differ between initiator and responder")
	}
	// SKEYSEED must be prf(Ni|Nr, g^ir) on both sides — recompute with the
	// responder's view to confirm.
	nr := resp.Payloads[2].(*ikev2.RawPayload).Data
	key := append(append([]byte{}, init.Ni...), nr...)
	want := init.prf.Compute(key, responderShared)
	if !bytes.Equal(init.ikeKeys.SKEYSEED, want) {
		t.Error("SKEYSEED mismatch against responder-computed shared secret")
	}
}

func TestHandleIKESAInitRespInvalidKE(t *testing.T) {
	init := newInitSession(t)
	init.buildIKESAInitPacket()
	respDH, _ := crypto.NewDiffieHellman(14)
	// INVALID_KE_PAYLOAD notify carrying DH group 21.
	notify := &ikev2.EncryptedPayloadNotify{ProtocolID: ikev2.ProtoIKE, NotifyType: notifyInvalidKE, NotifyData: []byte{0, 21}}
	resp := buildInitResp(t, init, respDH, notify)
	err := init.handleIKESAInitResp(resp)
	var keErr *ErrInvalidKEGroup
	if !errors.As(err, &keErr) || keErr.Group != 21 {
		t.Fatalf("err = %v, want ErrInvalidKEGroup{21}", err)
	}
}

func TestHandleIKESAInitRespCookie(t *testing.T) {
	init := newInitSession(t)
	init.buildIKESAInitPacket()
	respDH, _ := crypto.NewDiffieHellman(14)
	cookie := []byte{0xde, 0xad, 0xbe, 0xef}
	notify := &ikev2.EncryptedPayloadNotify{ProtocolID: ikev2.ProtoIKE, NotifyType: notifyCookie, NotifyData: cookie}
	resp := buildInitResp(t, init, respDH, notify)
	if err := init.handleIKESAInitResp(resp); !errors.Is(err, errCookieRequired) {
		t.Fatalf("err = %v, want errCookieRequired", err)
	}
	if !bytes.Equal(init.cookie, cookie) {
		t.Error("cookie not stored from COOKIE notify")
	}
	// The next IKE_SA_INIT must carry the cookie.
	pkt, err := init.buildIKESAInitPacket()
	if err != nil {
		t.Fatalf("resend build: %v", err)
	}
	var hasCookie bool
	for _, pl := range pkt.Payloads {
		if n, ok := pl.(*ikev2.EncryptedPayloadNotify); ok && n.NotifyType == notifyCookie {
			hasCookie = true
		}
	}
	if !hasCookie {
		t.Error("resend IKE_SA_INIT does not carry the COOKIE notify")
	}
}

func TestHandleIKESAInitRespRedirect(t *testing.T) {
	init := newInitSession(t)
	init.buildIKESAInitPacket()
	respDH, _ := crypto.NewDiffieHellman(14)
	// REDIRECTED_TO with an FQDN gateway.
	notify := &ikev2.EncryptedPayloadNotify{ProtocolID: ikev2.ProtoIKE, NotifyType: notifyRedirectedTo, NotifyData: []byte{3, 'e', 'p', 'd', 'g', '.', 'x'}}
	resp := buildInitResp(t, init, respDH, notify)
	err := init.handleIKESAInitResp(resp)
	var redir *RedirectError
	if !errors.As(err, &redir) || redir.Target != "epdg.x" {
		t.Fatalf("err = %v, want RedirectError{epdg.x}", err)
	}
}

func TestHandleIKESAInitRespMissingKE(t *testing.T) {
	init := newInitSession(t)
	init.buildIKESAInitPacket()
	// Build a response without a KE payload.
	pkt := &ikev2.IKEPacket{
		InitiatorSPI: init.SPIi, Version: 0x20, ExchangeType: ikev2.ExchangeIKEInit, Flags: 0x20,
		Payloads: []ikev2.Payload{
			&ikev2.EncryptedPayloadNonce{Data: bytes.Repeat([]byte{1}, 32)},
		},
	}
	dec, _ := ikev2.DecodePacket(pkt.Encode())
	if err := init.handleIKESAInitResp(dec); err == nil {
		t.Error("missing KE payload should error")
	}
}