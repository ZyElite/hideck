//go:build linux

package driver

import (
	"net"
	"reflect"
	"testing"

	"github.com/iniwex5/netlink"
)

func TestBuildAddXfrmStateRestoresOriginalFields(t *testing.T) {
	config := XFRMSAConfig{
		Src: net.ParseIP("192.0.2.1"), Dst: net.ParseIP("198.51.100.2"), SPI: 0x10203040,
		Proto: netlink.XFRM_PROTO_ESP, Mode: netlink.XFRM_MODE_TUNNEL,
		CryptAlgoName: "cbc(aes)", CryptKey: []byte{1, 2},
		AuthAlgoName: "hmac(sha1)", AuthKey: []byte{3, 4}, AuthTruncLen: 96,
		EncapType: netlink.XFRM_ENCAP_ESPINUDP, EncapSrcPort: 4500, EncapDstPort: 4500,
		Ifid: 42, TimeLimitSoft: 3300, TimeLimitHard: 3600,
		SADir: netlink.XFRM_SA_DIR_OUT, ESN: true,
	}
	state := buildAddXfrmState(config)
	if state.Spi != int(config.SPI) || state.ReplayWindow != defaultAddReplayWindow || !state.AFUnspec {
		t.Fatalf("state identity/defaults = spi=%x replay=%d af_unspec=%v", state.Spi, state.ReplayWindow, state.AFUnspec)
	}
	if state.Ifid != config.Ifid || state.SADir != config.SADir || !state.ESN || state.Limits.TimeSoft != 3300 || state.Limits.TimeHard != 3600 {
		t.Fatalf("state options = %#v", state)
	}
	if state.Crypt == nil || state.Auth == nil || state.Auth.TruncateLen != 96 || state.Aead != nil {
		t.Fatalf("state algorithms = crypt=%#v auth=%#v aead=%#v", state.Crypt, state.Auth, state.Aead)
	}
	if state.Encap == nil || state.Encap.SrcPort != 4500 || state.Encap.DstPort != 4500 {
		t.Fatalf("state encapsulation = %#v", state.Encap)
	}
}

func TestCompatibleStatesRemovesUnsupportedExtensionsInOrder(t *testing.T) {
	state := &netlink.XfrmState{SADir: netlink.XFRM_SA_DIR_IN, AFUnspec: true}
	attempts := compatibleStates(state)
	if len(attempts) != 3 {
		t.Fatalf("compatible state count = %d", len(attempts))
	}
	actual := [][2]any{
		{attempts[0].SADir, attempts[0].AFUnspec},
		{attempts[1].SADir, attempts[1].AFUnspec},
		{attempts[2].SADir, attempts[2].AFUnspec},
	}
	expected := [][2]any{{netlink.SADir(0), true}, {netlink.XFRM_SA_DIR_IN, false}, {netlink.SADir(0), false}}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("compatible states = %#v", actual)
	}
	if state.SADir != netlink.XFRM_SA_DIR_IN || !state.AFUnspec {
		t.Fatal("compatibleStates mutated the source state")
	}
}

func TestBuildUpdateXfrmStateUsesRecoveredUpdateSemantics(t *testing.T) {
	manager := NewXFRMManager()
	state := manager.buildXfrmState(XFRMSAConfig{
		AuthAlgoName: "hmac(sha256)", AuthKey: []byte{1}, AuthTruncLen: 128,
		CryptAlgoName: "cbc(aes)", CryptKey: []byte{2},
	})
	if state.ReplayWindow != defaultUpdateReplayWindow {
		t.Fatalf("update replay window = %d", state.ReplayWindow)
	}
	if state.Auth == nil || state.Auth.TruncateLen != 0 || state.Crypt == nil {
		t.Fatalf("update algorithms = auth=%#v crypt=%#v", state.Auth, state.Crypt)
	}
}

func TestBuildXfrmPolicyPreservesTemplate(t *testing.T) {
	manager := NewXFRMManager()
	network := &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}
	config := XFRMSPConfig{
		Src: network, Dst: network, Dir: netlink.XFRM_DIR_OUT,
		TmplSrc: net.ParseIP("192.0.2.1"), TmplDst: net.ParseIP("198.51.100.2"),
		TmplProto: netlink.XFRM_PROTO_ESP, TmplMode: netlink.XFRM_MODE_TUNNEL,
		TmplSPI: 7, Ifid: 42,
	}
	policy := manager.buildXfrmPolicy(config)
	if policy.Dir != config.Dir || policy.Ifid != config.Ifid || len(policy.Tmpls) != 1 {
		t.Fatalf("policy = %#v", policy)
	}
	template := policy.Tmpls[0]
	if template.Spi != config.TmplSPI || template.Proto != config.TmplProto || template.Mode != config.TmplMode {
		t.Fatalf("policy template = %#v", template)
	}
}
