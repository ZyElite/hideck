package imscore

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
)

func TestRecoveredServiceAccessorsProjectNegotiatedState(t *testing.T) {
	service, err := New(&IMSConfig{
		DeviceID: "dev-compat", Registrar: "configured.example:5060",
		IMPI: "private@ims.example", IMPU: "sip:public@ims.example",
		Domain: "ims.example", Realm: "realm.example", LocalIP: net.ParseIP("192.0.2.10"),
		LocalPort: 5060, UserAgent: "VoHive-Test", PAccessNetworkInfo: "IEEE-802.11",
		EnableIPSec3GPP: disabledBoolPointer(),
		IMSRegisterTemplate: policy.IMSRegisterTemplate{
			VoiceSupportedHeader: "100rel", VoiceAllowHeader: "INVITE,ACK,BYE",
			VoiceAcceptContact: "*;+g.3gpp.icsi-ref", VoicePPreferredService: "mmtel",
			AccessType: "3gpp-ims", ContactParamOrder: []string{"access_type", "sip_instance"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(service.StopCurrent)
	service.mu.Lock()
	service.registrar = "active.example:5070"
	service.registrationTransport = "tcp"
	service.registrationRemote = &net.UDPAddr{IP: net.ParseIP("198.51.100.20"), Port: 5070}
	service.protectedClientPort = 16082
	service.protectedServerPort = 16083
	service.regSession = &registerSession{
		contactUser: "contact-id", serviceRoute: "<sip:route.example;lr>",
		security: &securityAgreement{
			client: securityMechanism{SPIC: 11, SPIS: 12, PortC: 16082, PortS: 16083},
			server: &securityMechanism{SPIC: 21, SPIS: 22, PortC: 6060, PortS: 6061},
		},
	}
	service.regState = regRegistered
	service.mu.Unlock()
	service.regStatus.Store(registrationRegistered)

	if service.GetIMPU() != "sip:public@ims.example" || service.GetIMSServerAddr() != "active.example:5070" {
		t.Fatalf("identity/server = %q/%q", service.GetIMPU(), service.GetIMSServerAddr())
	}
	if client, server := service.GetLocalPorts(); client != 16082 || server != 16083 {
		t.Fatalf("local ports = %d/%d", client, server)
	}
	if client, server := service.GetRemotePorts(); client != 6060 || server != 6061 {
		t.Fatalf("remote ports = %d/%d", client, server)
	}
	if lc, ls, rc, rs := service.GetSpiPairs(); lc != 11 || ls != 12 || rc != 21 || rs != 22 {
		t.Fatalf("SPIs = %d/%d/%d/%d", lc, ls, rc, rs)
	}
	if route := service.GetServiceRoute(); route != "<sip:route.example;lr>" {
		t.Fatalf("Service-Route = %q", route)
	}
	snapshot := service.Snapshot()
	if snapshot.ContactID != "contact-id" || snapshot.LocalPortC != 16082 ||
		snapshot.RemotePortS != 6061 || snapshot.Transport != "tcp" || !service.Session().Registered {
		t.Fatalf("snapshot=%+v session=%+v", snapshot, service.Session())
	}
	profile := service.VoiceProfile()
	profile.ContactParamOrder[0] = "mutated"
	if service.VoiceProfile().ContactParamOrder[0] != "access_type" {
		t.Fatal("VoiceProfile returned an aliased parameter order")
	}
}

func TestCommitRegisterSuccessRecordsOnlyNegotiatedGRUU(t *testing.T) {
	service := mustEventTestService(t)
	if service.GetPubGRUU() != "" || service.GetTempGRUU() != "" {
		t.Fatal("service fabricated a GRUU before registration")
	}
	session := &registerSession{contactUser: "contact", expires: time.Hour}
	response := &sipResponse{Headers: map[string]string{
		"Contact": `<sip:user@ims.example>;pub-gruu="sip:user@ims.example;gr=public";temp-gruu="sip:temp@ims.example;gr=temporary"`,
	}}
	if _, err := service.commitRegisterSuccess(response, session); err != nil {
		t.Fatalf("commitRegisterSuccess: %v", err)
	}
	if service.GetPubGRUU() != "sip:user@ims.example;gr=public" ||
		service.GetTempGRUU() != "sip:temp@ims.example;gr=temporary" {
		t.Fatalf("GRUU = %q/%q", service.GetPubGRUU(), service.GetTempGRUU())
	}
}

func TestRecoveredListenPacketHonorsCanceledContext(t *testing.T) {
	service := mustEventTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.ListenPacket(ctx, "udp", &net.UDPAddr{}); err != context.Canceled {
		t.Fatalf("ListenPacket error = %v, want context canceled", err)
	}
}
