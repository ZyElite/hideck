package imscore

import (
	"errors"
	"strings"
	"testing"

	"github.com/iniwex5/vowifi-go/internal/vowifi/ipsec3gpp"
	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
)

func TestSupportedSecurityClientMechanismsPreservesCanonicalCarrierOrder(t *testing.T) {
	template := policy.DefaultIMSRegisterTemplate()
	template.SecurityClientMechanisms = []policy.IPSec3GPPSecurityMechanism{
		{Alg: "HMAC(SHA1)", EAlg: "CBC(AES)", Prot: "", Mode: ""},
		{Alg: "hmac(md5)", EAlg: "ecb(cipher_null)", Prot: "ESP", Mode: "trans"},
		{Alg: "unsupported", EAlg: "aes-cbc", Prot: "esp", Mode: "trans"},
	}
	mechanisms := supportedSecurityClientMechanisms(template)
	if len(mechanisms) != 2 {
		t.Fatalf("mechanisms = %+v", mechanisms)
	}
	if mechanisms[0].Auth != ipsec3gpp.AuthHMACSHA196 || mechanisms[0].Encryption != ipsec3gpp.EncryptionAES ||
		mechanisms[1].Auth != "hmac-md5-96" || mechanisms[1].Encryption != ipsec3gpp.EncryptionNull {
		t.Fatalf("canonical order = %+v", mechanisms)
	}
}

func TestSupportedSecurityClientMechanismsFallsBackWhenCarrierListIsInvalid(t *testing.T) {
	template := policy.DefaultIMSRegisterTemplate()
	template.SecurityClientMechanisms = []policy.IPSec3GPPSecurityMechanism{
		{Alg: "bad", EAlg: "bad", Prot: "bad", Mode: "bad"},
	}
	if got := len(supportedSecurityClientMechanisms(template)); got != 6 {
		t.Fatalf("fallback mechanism count = %d, want 6", got)
	}
}

func TestSecurityClientHeaderValueMatchesOriginalFormats(t *testing.T) {
	client := securityMechanism{SPIC: 11, SPIS: 12, PortC: 13, PortS: 14}
	template := policy.DefaultIMSRegisterTemplate()
	template.SecurityClientMechanisms = []policy.IPSec3GPPSecurityMechanism{
		{Alg: "hmac(sha1)", EAlg: "cbc(aes)", Prot: "esp", Mode: "trans"},
	}
	withServer := securityClientHeaderValue(client, template)
	wantWithServer := "ipsec-3gpp; alg=hmac-sha-1-96; ealg=aes-cbc; spi-c=11; spi-s=12; port-c=13; port-s=14"
	if withServer != wantWithServer {
		t.Fatalf("server-param Security-Client = %q, want %q", withServer, wantWithServer)
	}
	template.SecurityClientIncludesServerParams = false
	withoutServer := securityClientHeaderValue(client, template)
	wantWithoutServer := "ipsec-3gpp; alg=hmac-sha-1-96; ealg=aes-cbc; prot=esp; spi-c=11; port-c=13"
	if withoutServer != wantWithoutServer {
		t.Fatalf("minimal Security-Client = %q, want %q", withoutServer, wantWithoutServer)
	}
}

func TestSelectSecurityServerHonorsStrictTemplate(t *testing.T) {
	template := policy.DefaultIMSRegisterTemplate()
	template.SecurityClientMechanisms = []policy.IPSec3GPPSecurityMechanism{
		{Alg: "hmac-sha-1-96", EAlg: "aes-cbc", Prot: "esp", Mode: "trans"},
	}
	header := securityServerOffer("hmac-md5-96", "aes-cbc", "q=1")
	template.StrictSecurityServerOffer = true
	if _, _, err := selectSecurityServerForTemplate(header, template); err == nil {
		t.Fatal("strict template accepted an unadvertised mechanism")
	}
	template.StrictSecurityServerOffer = false
	selected, _, err := selectSecurityServerForTemplate(header, template)
	if err != nil || selected.Auth != "hmac-md5-96" {
		t.Fatalf("loose selection = %+v, %v", selected, err)
	}
}

func TestSelectSecurityServerAppliesOriginalEncryptionPreference(t *testing.T) {
	header := strings.Join([]string{
		securityServerOffer("hmac-sha-1-96", "null", "q=1"),
		securityServerOffer("hmac-sha-1-96", "aes-cbc", "q=0.95"),
	}, ",")
	selected, _, err := selectSecurityServerForTemplate(header, policy.DefaultIMSRegisterTemplate())
	if err != nil {
		t.Fatal(err)
	}
	if selected.Encryption != ipsec3gpp.EncryptionAES {
		t.Fatalf("selected encryption = %q, want aes-cbc", selected.Encryption)
	}
}

func TestSecAgreeDecisionsMatchOriginalModes(t *testing.T) {
	cfg := &IMSConfig{}
	template := policy.DefaultIMSRegisterTemplate()
	template.SecAgreeMode = "auto"
	auto := decideSecAgreeAfterChallenge(cfg, template, false)
	if auto.useIPSec || auto.mode != securityModePlain || auto.reason != securityAutoFallback || auto.err != nil {
		t.Fatalf("auto decision = %+v", auto)
	}
	template.SecAgreeMode = "required"
	required := decideSecAgreeAfterChallenge(cfg, template, false)
	if required.mode != securityModeIPSec || required.reason != securityRequired ||
		!errors.Is(required.err, errMissingUsableSecurityServer) {
		t.Fatalf("required decision = %+v", required)
	}
	template.SecAgreeMode = "disabled"
	disabled := decideSecAgreeAfterChallenge(cfg, template, true)
	if disabled.useIPSec || disabled.mode != securityModePlain || disabled.reason != securityDisabled {
		t.Fatalf("disabled decision = %+v", disabled)
	}
}

func TestValidateSecAgreeRegisterParamsUsesRecoveredError(t *testing.T) {
	err := validateSecAgreeRegisterParams(true, " ")
	if err == nil || err.Error() != missingSecurityClientForSecAgree || !isMissingSecurityClientForSecAgree(err) {
		t.Fatalf("validation error = %v", err)
	}
	if err := validateSecAgreeRegisterParams(false, ""); err != nil {
		t.Fatalf("disabled validation = %v", err)
	}
}

func TestIPSec3GPPOfferEqualForSAIncludesParameters(t *testing.T) {
	left := securityMechanism{
		Name: "IPSEC-3GPP", Auth: "HMAC(SHA1)", Encryption: "AES-CBC",
		SPIC: 1, SPIS: 2, PortC: 3, PortS: 4,
	}
	right := securityMechanism{
		Name: "ipsec-3gpp", Auth: "hmac(sha1)", Encryption: "aes-cbc", Protocol: "esp", Mode: "trans",
		SPIC: 1, SPIS: 2, PortC: 3, PortS: 4,
	}
	if !ipsec3gppOfferEqualForSA(left, right) {
		t.Fatal("equivalent SA offers did not match")
	}
	right.SPIS++
	if ipsec3gppOfferEqualForSA(left, right) {
		t.Fatal("different SA parameters matched")
	}
}

func securityServerOffer(auth, encryption, quality string) string {
	return "ipsec-3gpp;alg=" + auth + ";ealg=" + encryption +
		";prot=esp;mod=trans;spi-c=21;spi-s=22;port-c=23;port-s=24;" + quality
}
