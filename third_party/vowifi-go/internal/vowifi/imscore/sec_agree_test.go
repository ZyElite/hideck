package imscore

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/ipsec3gpp"
)

type captureIPSecNetwork struct {
	*SystemIMSNetwork
	policy    ipsec3gpp.Policy
	installed bool
}

func (n *captureIPSecNetwork) InstallIPSec3GPP(policy ipsec3gpp.Policy) error {
	validated, err := ipsec3gpp.NewPolicy(policy)
	if err != nil {
		return err
	}
	n.policy = validated
	n.installed = true
	return nil
}

func TestRegisterNegotiatesAndInstallsIPSec3GPP(t *testing.T) {
	network := &captureIPSecNetwork{SystemIMSNetwork: NewSystemIMSNetwork(net.IPv4(10, 0, 0, 2))}
	svc := newSecurityAgreementTestService(t, network)
	var initialClient string
	serverHeader := ""
	svc.transport.SetSendFn(func(request string) error {
		if strings.Contains(request, "CSeq: 1 REGISTER") {
			initialClient = sipHeaderValue(request, "Security-Client")
			assertInitialSecurityHeaders(t, request, initialClient)
			serverHeader = serverOfferForClient(t, initialClient)
			svc.transport.DeliverResponse(akaChallengeResponse(request, serverHeader))
			return nil
		}
		if !network.installed {
			t.Fatal("authenticated REGISTER sent before IPsec policy installation")
		}
		assertAuthenticatedSecurityHeaders(t, request, initialClient, serverHeader)
		if got := svc.currentRegistrationRemote().Port; got != 51001 {
			t.Fatalf("protected registrar port = %d, want 51001", got)
		}
		svc.transport.DeliverResponse(registerResponseForRequest(request, 200, nil))
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.Register(ctx); err != nil {
		t.Fatalf("Register: %v", err)
	}
	assertInstalledPolicy(t, network.policy)
	if got := svc.GetSecurityVerify(); got != serverHeader {
		t.Fatalf("Security-Verify = %q, want %q", got, serverHeader)
	}
}

func newSecurityAgreementTestService(t *testing.T, network IMSNetwork) *Service {
	t.Helper()
	svc, err := New(&IMSConfig{
		DeviceID: "dev-sec", IMEI: "356938035643809", IMSI: "310260123456789", IMPI: "310260123456789@ims.example",
		IMPU: []string{"sip:310260123456789@ims.example"}, Domain: "ims.example",
		LocalIP: net.IPv4(10, 0, 0, 2), LocalPort: 41000, Transport: "udp", Expires: time.Hour,
		AKAProvider: stubAKAProvider{}, IMSNetwork: network, IPSec3GPPEnabled: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	svc.protectedServerPort = 41001
	svc.registrationRemote = &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 5060}
	t.Cleanup(svc.Stop)
	return svc
}

func assertInitialSecurityHeaders(t *testing.T, request, securityClient string) {
	t.Helper()
	if securityClient == "" || !strings.Contains(securityClient, "ealg=aes-cbc") || !strings.Contains(securityClient, "ealg=null") {
		t.Fatalf("initial Security-Client is invalid: %q", securityClient)
	}
	if sipHeaderValue(request, "Require") != "sec-agree" || sipHeaderValue(request, "Proxy-Require") != "sec-agree" {
		t.Fatalf("initial REGISTER did not require sec-agree: %q", request)
	}
	if sipHeaderValue(request, "Security-Verify") != "" {
		t.Fatal("initial REGISTER unexpectedly contained Security-Verify")
	}
	if !strings.Contains(sipHeaderValue(request, "Via"), "10.0.0.2:41000;") ||
		!strings.Contains(sipHeaderValue(request, "Contact"), "@10.0.0.2:41000>") {
		t.Fatal("initial REGISTER did not use the unprotected client port")
	}
	if !strings.Contains(sipHeaderValue(request, "Authorization"), `nonce=""`) {
		t.Fatal("initial REGISTER omitted the empty IMS AKA authorization")
	}
}

func serverOfferForClient(t *testing.T, clientHeader string) string {
	t.Helper()
	client, err := parseSecurityMechanism(splitSecurityMechanisms(clientHeader)[0])
	if err != nil {
		t.Fatalf("parse generated Security-Client: %v", err)
	}
	spiC, spiS := uint32(0x33333333), uint32(0x44444444)
	if client.SPIC == spiC || client.SPIS == spiC {
		spiC++
	}
	if client.SPIC == spiS || client.SPIS == spiS || spiC == spiS {
		spiS++
	}
	return fmt.Sprintf("ipsec-3gpp;alg=hmac-sha-1-96;ealg=aes-cbc;prot=esp;mod=trans;spi-c=%d;spi-s=%d;port-c=51000;port-s=51001", spiC, spiS)
}

func akaChallengeResponse(request, securityServer string) *sipResponse {
	randBytes := bytes.Repeat([]byte{0x11}, 16)
	autnBytes := bytes.Repeat([]byte{0x22}, 16)
	nonce := base64Std(append(append([]byte{}, randBytes...), autnBytes...))
	headers := map[string]string{
		"WWW-Authenticate": `Digest realm="ims.example", nonce="` + nonce + `", algorithm=AKAv1-MD5, qop="auth"`,
	}
	if securityServer != "" {
		headers["Security-Server"] = securityServer
	}
	return registerResponseForRequest(request, 401, headers)
}

func assertAuthenticatedSecurityHeaders(t *testing.T, request, client, server string) {
	t.Helper()
	if sipHeaderValue(request, "Security-Client") != client {
		t.Fatal("authenticated REGISTER changed Security-Client")
	}
	if sipHeaderValue(request, "Security-Verify") != server {
		t.Fatal("authenticated REGISTER did not mirror Security-Server")
	}
	if sipHeaderValue(request, "Authorization") == "" {
		t.Fatal("authenticated REGISTER omitted Digest-AKA authorization")
	}
	if !strings.Contains(sipHeaderValue(request, "Via"), "10.0.0.2:41001;") ||
		!strings.Contains(sipHeaderValue(request, "Contact"), "@10.0.0.2:41001>") {
		t.Fatal("authenticated REGISTER did not advertise the protected server port")
	}
}

func assertInstalledPolicy(t *testing.T, policy ipsec3gpp.Policy) {
	t.Helper()
	if !policy.LocalIP.Equal(net.IPv4(10, 0, 0, 2)) || !policy.RemoteIP.Equal(net.IPv4(10, 0, 0, 1)) {
		t.Fatalf("policy endpoints = %s -> %s", policy.LocalIP, policy.RemoteIP)
	}
	if policy.LocalClientPort != 41000 || policy.LocalServerPort != 41001 ||
		policy.RemoteClientPort != 51000 || policy.RemoteServerPort != 51001 {
		t.Fatalf("policy ports = %+v", policy)
	}
	if policy.Encryption != ipsec3gpp.EncryptionAES || !bytes.Equal(policy.CK, bytes.Repeat([]byte{0x11}, 16)) || !bytes.Equal(policy.IK, bytes.Repeat([]byte{0x22}, 16)) {
		t.Fatal("policy did not retain negotiated algorithm and AKA keys")
	}
}

func TestRegisterRejectsMissingSecurityServer(t *testing.T) {
	network := &captureIPSecNetwork{SystemIMSNetwork: NewSystemIMSNetwork(net.IPv4(10, 0, 0, 2))}
	svc := newSecurityAgreementTestService(t, network)
	svc.transport.SetSendFn(func(request string) error {
		svc.transport.DeliverResponse(akaChallengeResponse(request, ""))
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := svc.Register(ctx)
	if err == nil || !strings.Contains(err.Error(), "missing Security-Server") {
		t.Fatalf("Register error = %v, want missing Security-Server", err)
	}
}
