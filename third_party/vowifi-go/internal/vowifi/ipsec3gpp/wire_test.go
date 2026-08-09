package ipsec3gpp

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"testing"
)

func TestTransportProducesAuthenticatedESPWireFormat(t *testing.T) {
	policy := testPolicy(nil, nil, EncryptionNull)
	transport, err := NewTransport(policy)
	if err != nil {
		t.Fatalf("NewTransport: %v", err)
	}
	packet := udpPacket(t, policy.LocalIP, 41000, policy.RemoteIP, 51001, []byte("REGISTER"))
	protected, transformed, err := transport.TransformOutbound(packet)
	if err != nil || !transformed {
		t.Fatalf("TransformOutbound = transformed:%t err:%v", transformed, err)
	}
	parsed, err := parseIPPacket(protected)
	if err != nil {
		t.Fatalf("parse protected packet: %v", err)
	}
	esp := parsed.payload
	if binary.BigEndian.Uint32(esp[:4]) != policy.FlowC.OutboundSPI || binary.BigEndian.Uint32(esp[4:8]) != 1 {
		t.Fatalf("ESP header = %x", esp[:8])
	}
	assertSHA1ICV(t, esp, policy.FlowC.IK)

	icvOffset := len(esp) - 12
	plain := esp[8:icvOffset]
	original, err := parseIPPacket(packet)
	if err != nil {
		t.Fatalf("parse original packet: %v", err)
	}
	if !bytes.HasPrefix(plain, original.payload) || plain[len(plain)-1] != protocolUDP {
		t.Fatalf("null ESP payload/trailer = %x", plain)
	}
	padLength := int(plain[len(plain)-2])
	for index, value := range plain[len(original.payload) : len(plain)-2] {
		if value != byte(index+1) {
			t.Fatalf("padding byte %d = %d", index, value)
		}
	}
	if padLength != len(plain)-len(original.payload)-2 {
		t.Fatalf("padding length = %d", padLength)
	}
}

func TestTransportSupportsNegotiatedAlgorithmCombinations(t *testing.T) {
	tests := []struct {
		name, authentication, encryption string
	}{
		{name: "AES SHA1", authentication: AuthHMACSHA196, encryption: EncryptionAES},
		{name: "3DES SHA1", authentication: "hmac(sha1)", encryption: Encryption3DES},
		{name: "null MD5", authentication: "hmac-md5-96", encryption: EncryptionNull},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uePolicy := testPolicy(nil, nil, test.encryption)
			setPolicyAuthentication(&uePolicy, test.authentication)
			ue, server := transportPairFromPolicy(t, uePolicy)
			request := udpPacket(t, uePolicy.LocalIP, 41000, uePolicy.RemoteIP, 51001, []byte(test.name))
			protected, transformed, err := ue.TransformOutbound(request)
			if err != nil || !transformed {
				t.Fatalf("protect = transformed:%t err:%v", transformed, err)
			}
			decoded, transformed, err := server.TransformInbound(protected)
			if err != nil || !transformed || !bytes.Equal(decoded, request) {
				t.Fatalf("round trip = transformed:%t err:%v", transformed, err)
			}
		})
	}
}

func assertSHA1ICV(t *testing.T, esp, ik []byte) {
	t.Helper()
	key := make([]byte, 20)
	copy(key, ik[:16])
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(esp[:len(esp)-12])
	want := mac.Sum(nil)[:12]
	if got := esp[len(esp)-12:]; !hmac.Equal(got, want) {
		t.Fatalf("ICV = %x, want %x", got, want)
	}
}

func setPolicyAuthentication(policy *Policy, authentication string) {
	policy.FlowC.AuthAlg = authentication
	policy.FlowS.AuthAlg = authentication
}

func transportPairFromPolicy(t *testing.T, uePolicy Policy) (*Transport, *Transport) {
	t.Helper()
	serverPolicy := Policy{
		LocalIP: uePolicy.RemoteIP, RemoteIP: uePolicy.LocalIP,
		LocalPortC: uePolicy.RemotePortC, LocalPortS: uePolicy.RemotePortS,
		RemotePortC: uePolicy.LocalPortC, RemotePortS: uePolicy.LocalPortS,
		FlowC: reverseFlow(uePolicy.FlowS), FlowS: reverseFlow(uePolicy.FlowC),
	}
	ue, err := NewTransport(uePolicy)
	if err != nil {
		t.Fatalf("new UE transport: %v", err)
	}
	server, err := NewTransport(serverPolicy)
	if err != nil {
		t.Fatalf("new server transport: %v", err)
	}
	return ue, server
}
