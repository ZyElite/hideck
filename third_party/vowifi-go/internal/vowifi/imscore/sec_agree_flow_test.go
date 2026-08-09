package imscore

import (
	"bytes"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/ipsec3gpp"
	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
)

type removableCaptureNetwork struct {
	*captureIPSecNetwork
	removals int
}

func (network *removableCaptureNetwork) RemoveIPSec3GPP() error {
	network.removals++
	network.installed = false
	return nil
}

func TestRegisterAutoSecAgreeFallsBackToPlainAfterMissingOffer(t *testing.T) {
	network := &captureIPSecNetwork{SystemIMSNetwork: NewSystemIMSNetwork(testLocalIP)}
	svc := newSecurityAgreementTestService(t, network)
	template := policy.DefaultIMSRegisterTemplate()
	template.SecAgreeMode = "auto"
	svc.cfg.IMSRegisterTemplate = template
	requests := captureRegisterRequests(svc, func(request string, number int) *sipResponse {
		if number == 1 {
			return akaChallengeResponse(request, "")
		}
		return registerResponseForRequest(request, 200, nil)
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.Register(ctx); err != nil {
		t.Fatalf("Register: %v", err)
	}
	initial, authenticated := requests()
	if rawSIPHeaderValue(initial, "Security-Client") == "" {
		t.Fatal("initial REGISTER omitted Security-Client")
	}
	if rawSIPHeaderValue(authenticated, "Security-Client") != "" ||
		rawSIPHeaderValue(authenticated, "Require") != "" {
		t.Fatalf("plain fallback retained sec-agree headers: %q", authenticated)
	}
	status := svc.Status()
	if status.EffectiveSecurityMode != securityModePlain || status.SecurityFallbackReason != securityAutoFallback ||
		status.SecurityFallbackCount != 1 || !status.SignalingReady {
		t.Fatalf("status after fallback = %+v", status)
	}
}

func TestRegisterDisabledSecAgreeSendsPlainRequest(t *testing.T) {
	svc := newSecurityAgreementTestService(t, NewSystemIMSNetwork(testLocalIP))
	template := policy.DefaultIMSRegisterTemplate()
	template.SecAgreeMode = "disabled"
	svc.cfg.IMSRegisterTemplate = template
	requests := captureRegisterRequests(svc, func(request string, _ int) *sipResponse {
		return registerResponseForRequest(request, 200, nil)
	})
	if err := svc.Register(context.Background()); err != nil {
		t.Fatal(err)
	}
	request, _ := requests()
	if rawSIPHeaderValue(request, "Security-Client") != "" || rawSIPHeaderValue(request, "Require") != "" ||
		strings.Contains(strings.ToLower(rawSIPHeaderValue(request, "Supported")), "sec-agree") {
		t.Fatalf("disabled request retained sec-agree: %q", request)
	}
}

func TestRequiredSecAgreeRejectsInitialSuccess(t *testing.T) {
	svc := newSecurityAgreementTestService(t, NewSystemIMSNetwork(testLocalIP))
	template := policy.DefaultIMSRegisterTemplate()
	template.SecAgreeMode = "required"
	svc.cfg.IMSRegisterTemplate = template
	captureRegisterRequests(svc, func(request string, _ int) *sipResponse {
		return registerResponseForRequest(request, 200, map[string]string{
			"Security-Server": securityServerOffer("hmac-sha-1-96", "aes-cbc", "q=1"),
		})
	})
	err := svc.Register(context.Background())
	if err == nil || !strings.Contains(err.Error(), "initial_200_security_server_without_ipsec_install_required") {
		t.Fatalf("Register error = %v", err)
	}
}

func TestInitialRejectFallbackRebuildsSecurityClientFromFallbackTemplate(t *testing.T) {
	svc := newSecurityAgreementTestService(t, NewSystemIMSNetwork(testLocalIP))
	template := policy.DefaultIMSRegisterTemplate()
	template.FallbackIncludesServerParamsInSecCl = false
	svc.cfg.IMSRegisterTemplate = template
	requests := captureRegisterRequests(svc, func(request string, number int) *sipResponse {
		status := 403
		if number == 2 {
			status = 200
		}
		return registerResponseForRequest(request, status, nil)
	})
	if err := svc.Register(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, second := requests()
	firstSecurity := rawSIPHeaderValue(first, "Security-Client")
	secondSecurity := rawSIPHeaderValue(second, "Security-Client")
	if !strings.Contains(firstSecurity, "spi-s=") || strings.Contains(secondSecurity, "spi-s=") ||
		!strings.Contains(secondSecurity, "prot=esp") {
		t.Fatalf("fallback Security-Client formats = first %q, second %q", firstSecurity, secondSecurity)
	}
}

func TestRegisterUsesCarrierMechanismAndChallengePathForIPSec(t *testing.T) {
	network := &captureIPSecNetwork{SystemIMSNetwork: NewSystemIMSNetwork(testLocalIP)}
	svc := newSecurityAgreementTestService(t, network)
	template := policy.DefaultIMSRegisterTemplate()
	template.StrictSecurityServerOffer = true
	template.SecurityClientMechanisms = []policy.IPSec3GPPSecurityMechanism{
		{Alg: "hmac(md5)", EAlg: "cbc(des3_ede)", Prot: "esp", Mode: "trans"},
	}
	svc.cfg.IMSRegisterTemplate = template
	requests := captureRegisterRequests(svc, func(request string, number int) *sipResponse {
		if number == 1 {
			response := akaChallengeResponse(request, securityServerOffer("hmac(md5)", "cbc(des3_ede)", "q=1"))
			response.Headers["Path"] = "<sip:user@192.0.2.99:5060;lr>"
			return response
		}
		return registerResponseForRequest(request, 200, nil)
	})
	if err := svc.Register(context.Background()); err != nil {
		t.Fatal(err)
	}
	initial, _ := requests()
	clientHeader := rawSIPHeaderValue(initial, "Security-Client")
	if len(splitSecurityMechanisms(clientHeader)) != 1 || !strings.Contains(clientHeader, "alg=hmac-md5-96") ||
		!strings.Contains(clientHeader, "ealg=des-ede3-cbc") {
		t.Fatalf("carrier Security-Client = %q", clientHeader)
	}
	installed := network.policy
	if !installed.RemoteIP.Equal(net.ParseIP("192.0.2.99")) || installed.FlowC.AuthAlg != "hmac-md5-96" ||
		installed.FlowC.EncAlg != ipsec3gpp.Encryption3DES {
		t.Fatalf("installed carrier policy = %+v", installed)
	}
}

func TestInstallNegotiatedIPSecRollsBackWhenProtectedTCPFails(t *testing.T) {
	network := &removableCaptureNetwork{captureIPSecNetwork: &captureIPSecNetwork{
		SystemIMSNetwork: NewSystemIMSNetwork(testLocalIP),
	}}
	svc := newSecurityAgreementTestService(t, network)
	svc.externalTransport = false
	session := &registerSession{
		template: policy.DefaultIMSRegisterTemplate(),
		security: &securityAgreement{client: securityMechanism{
			Name: "ipsec-3gpp", Auth: ipsec3gpp.AuthHMACSHA196, Encryption: ipsec3gpp.EncryptionAES,
			Protocol: ipsec3gpp.ProtocolESP, Mode: ipsec3gpp.ModeTransport,
			SPIC: 11, SPIS: 12, PortC: 41000, PortS: 41001,
		}},
	}
	response := &sipResponse{Headers: map[string]string{
		"Security-Server": securityServerOffer("hmac-sha-1-96", "aes-cbc", "q=1"),
	}}
	err := svc.installNegotiatedIPSec(context.Background(), session, response, AKAResult{
		CK: bytes.Repeat([]byte{0x11}, 16), IK: bytes.Repeat([]byte{0x22}, 16),
	})
	if err == nil || !strings.Contains(err.Error(), "protected client port was not reserved") {
		t.Fatalf("install error = %v", err)
	}
	if network.removals != 1 || network.installed {
		t.Fatalf("rollback state = removals %d installed %t", network.removals, network.installed)
	}
	remote := svc.currentRegistrationRemote()
	if remote == nil || remote.Port != 5060 || !remote.IP.Equal(net.IPv4(10, 0, 0, 1)) {
		t.Fatalf("registrar after rollback = %v", remote)
	}
}

var testLocalIP = net.IPv4(10, 0, 0, 2)

func captureRegisterRequests(
	svc *Service,
	respond func(string, int) *sipResponse,
) func() (string, string) {
	var mu sync.Mutex
	requests := make([]string, 0, 2)
	svc.transport.SetSendFn(func(request string) error {
		if !strings.HasPrefix(request, "REGISTER ") {
			return nil
		}
		mu.Lock()
		requests = append(requests, request)
		number := len(requests)
		mu.Unlock()
		svc.transport.DeliverResponse(respond(request, number))
		return nil
	})
	return func() (string, string) {
		mu.Lock()
		defer mu.Unlock()
		var first, second string
		if len(requests) > 0 {
			first = requests[0]
		}
		if len(requests) > 1 {
			second = requests[1]
		}
		return first, second
	}
}
