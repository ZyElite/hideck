package swu

import (
	"net"
	"strings"
	"testing"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func TestParseAssignedInnerConfigRejectsMissingReply(t *testing.T) {
	_, err := parseAssignedInnerConfig([]ikev2.Payload{
		&ikev2.EncryptedPayloadAuth{AuthMethod: ikev2.AuthMethodDigitalSignature},
	})
	if err == nil || !strings.Contains(err.Error(), "omitted CFG_REPLY (payloads=39)") {
		t.Fatalf("parseAssignedInnerConfig error = %v", err)
	}
}

func TestParseAssignedInnerConfigReportsReplyAttributesWithoutAddress(t *testing.T) {
	cp := &ikev2.EncryptedPayloadCP{
		ConfigType: ikev2.CPTypeReply,
		Attrs: []*ikev2.CPAttribute{
			{Type: ikev2.CPAttrIP4DNS, Value: net.IPv4(1, 1, 1, 1).To4()},
		},
	}
	_, err := parseAssignedInnerConfig([]ikev2.Payload{cp})
	if err == nil || !strings.Contains(err.Error(), "attributes=3:4") {
		t.Fatalf("parseAssignedInnerConfig error = %v", err)
	}
}

func TestParseAssignedInnerConfigAcceptsIPv4Reply(t *testing.T) {
	cp := &ikev2.EncryptedPayloadCP{
		ConfigType: ikev2.CPTypeReply,
		Attrs: []*ikev2.CPAttribute{
			{Type: ikev2.CPAttrIP4Address, Value: net.IPv4(10, 0, 0, 8).To4()},
		},
	}
	config, err := parseAssignedInnerConfig([]ikev2.Payload{cp})
	if err != nil {
		t.Fatalf("parseAssignedInnerConfig: %v", err)
	}
	if got := config.ipv4.String(); got != "10.0.0.8" {
		t.Fatalf("assigned IPv4 = %q", got)
	}
}

func TestParseAssignedInnerConfigRejectsIPv6OnlyReply(t *testing.T) {
	cp := &ikev2.EncryptedPayloadCP{
		ConfigType: ikev2.CPTypeReply,
		Attrs: []*ikev2.CPAttribute{
			{Type: ikev2.CPAttrIP6Address, Value: net.ParseIP("2001:db8::8").To16()},
		},
	}
	_, err := parseAssignedInnerConfig([]ikev2.Payload{cp})
	if err == nil || !strings.Contains(err.Error(), "assigned only IPv6 2001:db8::8") {
		t.Fatalf("parseAssignedInnerConfig error = %v", err)
	}
}

func TestIKEAuthenticationErrorReportsAddressFailure(t *testing.T) {
	wire := ikev2.EncodePayloadChain([]ikev2.Payload{
		&ikev2.EncryptedPayloadNotify{NotifyType: 36},
	})
	payloads, err := ikev2.DecodePayloadChainWithFirst(ikev2.PayloadNotify, wire)
	if err != nil {
		t.Fatalf("decode Notify: %v", err)
	}
	err = ikeAuthenticationError(payloads)
	if err == nil || !strings.Contains(err.Error(), "INTERNAL_ADDRESS_FAILURE (36)") {
		t.Fatalf("ikeAuthenticationError = %v", err)
	}
}
