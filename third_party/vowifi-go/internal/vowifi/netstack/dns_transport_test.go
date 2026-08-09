package netstack

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"gvisor.dev/gvisor/pkg/tcpip/checksum"
	"gvisor.dev/gvisor/pkg/tcpip/header"
)

func TestTunnelDNSResolvesAAndAAAAAndSRV(t *testing.T) {
	packetIO := newChannelPacketIO()
	adapter, err := NewTunnelNetwork(
		net.IPv4(10, 0, 0, 2), 32, []string{"10.0.0.53"}, packetIO,
	)
	if err != nil {
		t.Fatalf("NewTunnelNetwork: %v", err)
	}
	defer adapter.Close()
	serveDNSPackets(t, packetIO, 3)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if got, err := adapter.ResolveIP(ctx, "pcscf.example"); err != nil || !got.Equal(net.IPv4(192, 0, 2, 10)) {
		t.Fatalf("ResolveIP A = %v, %v", got, err)
	}
	got, err := adapter.Network().ResolveIP(ctx, "pcscf6.example", true)
	if err != nil || !got.Equal(net.ParseIP("2001:db8::10")) {
		t.Fatalf("ResolveIP AAAA = %v, %v", got, err)
	}
	target, port, err := adapter.LookupSRV(ctx, "sip", "udp", "ims.example")
	if err != nil || target != "pcscf.example" || port != 5060 {
		t.Fatalf("LookupSRV = %q, %d, %v", target, port, err)
	}
}

func serveDNSPackets(t *testing.T, packetIO *channelPacketIO, count int) {
	t.Helper()
	go func() {
		for range count {
			select {
			case request := <-packetIO.outbound:
				response, err := dnsResponsePacket(request)
				if err != nil {
					t.Errorf("build DNS response: %v", err)
					return
				}
				packetIO.inbound <- response
			case <-time.After(time.Second):
				t.Error("DNS query did not reach SWu endpoint")
				return
			}
		}
	}()
}

func dnsResponsePacket(request []byte) ([]byte, error) {
	if len(request) < header.IPv4MinimumSize+header.UDPMinimumSize {
		return nil, fmt.Errorf("short request: %d", len(request))
	}
	ipHeader := header.IPv4(request)
	headerLength := int(ipHeader.HeaderLength())
	udpHeader := header.UDP(request[headerLength:])
	query := new(mdns.Msg)
	if err := query.Unpack(udpHeader.Payload()); err != nil {
		return nil, err
	}
	if len(query.Question) != 1 {
		return nil, fmt.Errorf("question count = %d", len(query.Question))
	}
	response := new(mdns.Msg)
	response.SetReply(query)
	question := query.Question[0]
	switch question.Qtype {
	case mdns.TypeA:
		response.Answer = []mdns.RR{&mdns.A{
			Hdr: mdns.RR_Header{Name: question.Name, Rrtype: mdns.TypeA, Class: mdns.ClassINET},
			A:   net.IPv4(192, 0, 2, 10),
		}}
	case mdns.TypeAAAA:
		response.Answer = []mdns.RR{&mdns.AAAA{
			Hdr:  mdns.RR_Header{Name: question.Name, Rrtype: mdns.TypeAAAA, Class: mdns.ClassINET},
			AAAA: net.ParseIP("2001:db8::10"),
		}}
	case mdns.TypeSRV:
		response.Answer = []mdns.RR{
			&mdns.SRV{
				Hdr:      mdns.RR_Header{Name: question.Name, Rrtype: mdns.TypeSRV, Class: mdns.ClassINET},
				Priority: 10, Port: 5070, Target: "lower-priority.example.",
			},
			&mdns.SRV{
				Hdr:      mdns.RR_Header{Name: question.Name, Rrtype: mdns.TypeSRV, Class: mdns.ClassINET},
				Priority: 1, Port: 5060, Target: "pcscf.example.",
			},
		}
	default:
		return nil, fmt.Errorf("unsupported query type %d", question.Qtype)
	}
	payload, err := response.Pack()
	if err != nil {
		return nil, err
	}
	return makeIPv4UDPResponse(ipHeader, udpHeader, payload), nil
}

func makeIPv4UDPResponse(requestIP header.IPv4, requestUDP header.UDP, payload []byte) []byte {
	totalLength := header.IPv4MinimumSize + header.UDPMinimumSize + len(payload)
	packet := make([]byte, totalLength)
	source := requestIP.DestinationAddress()
	destination := requestIP.SourceAddress()
	ipHeader := header.IPv4(packet)
	ipHeader.Encode(&header.IPv4Fields{
		TotalLength: uint16(totalLength), TTL: 64, Protocol: uint8(header.UDPProtocolNumber),
		SrcAddr: source, DstAddr: destination,
	})
	ipHeader.SetChecksum(^ipHeader.CalculateChecksum())
	udpHeader := header.UDP(packet[header.IPv4MinimumSize:])
	udpLength := uint16(header.UDPMinimumSize + len(payload))
	udpHeader.SetSourcePort(requestUDP.DestinationPort())
	udpHeader.SetDestinationPort(requestUDP.SourcePort())
	udpHeader.SetLength(udpLength)
	copy(udpHeader.Payload(), payload)
	pseudo := header.PseudoHeaderChecksum(header.UDPProtocolNumber, source, destination, udpLength)
	pseudo = checksum.Checksum(payload, pseudo)
	udpHeader.SetChecksum(^udpHeader.CalculateChecksum(pseudo))
	return packet
}
