package swu

import (
	"bytes"
	"context"
	"testing"
	"time"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
)

func TestCreateInitialChildSAPreservesConfiguredPFS(t *testing.T) {
	session := NewSession(&Config{})
	transport := newTestIKETransport()
	session.socket = transport
	copy(session.spiI[:], []byte("init-spi"))
	copy(session.spiR[:], []byte("resp-spi"))
	session.ikeKeys = testIKEKeys()
	session.ikeKeys.SK_d = bytes.Repeat([]byte{0x31}, enginecrypto.PRFOutputSize(session.prf))
	session.innerIP = []byte{10, 0, 0, 2}
	oldDH, err := enginecrypto.NewDiffieHellman(14)
	if err != nil || oldDH.GenerateKey() != nil {
		t.Fatalf("initial child DH: %v", err)
	}
	session.childDH = oldDH
	go respondToChildSARekeyResponse(
		t, session, transport, 0xa1b2c3d4, bytes.Repeat([]byte{0x92}, 32),
	)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.createInitialChildSA(ctx); err != nil {
		t.Fatalf("createInitialChildSA: %v", err)
	}
	if session.childDH == nil || session.childDH == oldDH || session.childDH.Group != 14 {
		t.Fatalf("initial CHILD_SA DH = %#v", session.childDH)
	}
	if len(session.childDHSecret) == 0 {
		t.Fatal("initial CHILD_SA PFS secret was not installed")
	}
}
