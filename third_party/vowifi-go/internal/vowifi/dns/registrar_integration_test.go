package dns

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	miekgdns "github.com/miekg/dns"
)

func TestDiscoverRegistrarViaDNSQueriesSRVAndBothAddressFamilies(t *testing.T) {
	ip, port := startTestDNSServer(t, registrarDNSHandler(false))
	factory := func(bindIP, server net.IP) queryResolver {
		return newServerResolverAt(bindIP, server, port)
	}
	result, err := discoverRegistrarViaDNS(
		context.Background(), "ims.example", "tcp", false, ip, []net.IP{ip}, factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "192.0.2.20:5070,[2001:db8::20]:5070,192.0.2.10:5060,[2001:db8::10]:5060"
	if result != want {
		t.Fatalf("registrar = %q, want %q", result, want)
	}
}

func TestDiscoverRegistrarViaDNSUsesNAPTRReplacement(t *testing.T) {
	ip, port := startTestDNSServer(t, registrarDNSHandler(true))
	factory := func(bindIP, server net.IP) queryResolver {
		return newServerResolverAt(bindIP, server, port)
	}
	result, err := discoverRegistrarViaDNS(
		context.Background(), "naptr.example", "tcp", false, ip, []net.IP{ip}, factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result != "192.0.2.30:5090,[2001:db8::30]:5090" {
		t.Fatalf("registrar = %q", result)
	}
}

func TestLookupHostIPViaDNSServersHonorsContextTimeout(t *testing.T) {
	handler := miekgdns.HandlerFunc(func(writer miekgdns.ResponseWriter, request *miekgdns.Msg) {
		time.Sleep(200 * time.Millisecond)
		_ = writer.WriteMsg(new(miekgdns.Msg).SetReply(request))
	})
	ip, port := startTestDNSServer(t, handler)
	factory := func(bindIP, server net.IP) queryResolver {
		return newServerResolverAt(bindIP, server, port)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	result := lookupHostIPViaDNSServers(ctx, "slow.example", false, ip, []net.IP{ip}, factory)
	if len(result) != 0 {
		t.Fatalf("result = %v", result)
	}
	if elapsed := time.Since(started); elapsed > 150*time.Millisecond {
		t.Fatalf("lookup ignored context timeout: %v", elapsed)
	}
}

func TestLookupHostIPViaDNSServersAccumulatesUntilPreferredFamily(t *testing.T) {
	servers := []net.IP{net.ParseIP("192.0.2.53"), net.ParseIP("192.0.2.54")}
	factory := func(_, server net.IP) queryResolver {
		if server.Equal(servers[0]) {
			return staticQueryResolver{addresses: []net.IPAddr{{IP: net.ParseIP("2001:db8::1")}}}
		}
		return staticQueryResolver{addresses: []net.IPAddr{{IP: net.ParseIP("192.0.2.1")}}}
	}
	got := lookupHostIPViaDNSServers(
		context.Background(), "pcscf.example", false, nil, servers, factory,
	)
	if values := ipStrings(got); len(values) != 2 || values[0] != "192.0.2.1" || values[1] != "2001:db8::1" {
		t.Fatalf("addresses = %v", values)
	}
}

func TestDiscoverRegistrarWithOptionsReportsStageSource(t *testing.T) {
	ip, port := startTestDNSServer(t, registrarDNSHandler(false))
	factory := func(bindIP, server net.IP) queryResolver {
		return newServerResolverAt(bindIP, server, port)
	}
	result, err := discoverRegistrarWithOptions(
		context.Background(), "ims.example", "tcp", false, ip, []net.IP{ip},
		false, nil, false, nil, factory,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "dns" || !strings.HasPrefix(result.Registrar, "192.0.2.20:5070") {
		t.Fatalf("result = %#v", result)
	}
}

func TestStructuredDiscoveryFallsBackToAAndAAAA(t *testing.T) {
	ip, port := startTestDNSServer(t, registrarDNSHandler(false))
	candidates, err := DiscoverRegistrarCandidatesViaDNS(
		"fallback.example", []string{net.JoinHostPort(ip.String(), port)}, time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0].Port != 5060 || candidates[1].Port != 5060 {
		t.Fatalf("candidates = %#v", candidates)
	}
}

type staticQueryResolver struct {
	addresses []net.IPAddr
}

func (resolver staticQueryResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return resolver.addresses, nil
}

func (staticQueryResolver) LookupSRV(context.Context, string, string, string) (string, []*net.SRV, error) {
	return "", nil, nil
}

func (staticQueryResolver) LookupNAPTR(context.Context, string) ([]*miekgdns.NAPTR, error) {
	return nil, nil
}

func startTestDNSServer(t *testing.T, handler miekgdns.Handler) (net.IP, string) {
	t.Helper()
	packet, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &miekgdns.Server{PacketConn: packet, Handler: handler}
	go func() { _ = server.ActivateAndServe() }()
	t.Cleanup(func() { _ = server.Shutdown() })
	host, port, err := net.SplitHostPort(packet.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	return net.ParseIP(host), port
}

func registrarDNSHandler(useNAPTR bool) miekgdns.Handler {
	return miekgdns.HandlerFunc(func(writer miekgdns.ResponseWriter, request *miekgdns.Msg) {
		response := new(miekgdns.Msg)
		response.SetReply(request)
		response.Authoritative = true
		for _, question := range request.Question {
			appendRegistrarDNSAnswers(response, question, useNAPTR)
		}
		_ = writer.WriteMsg(response)
	})
}

func appendRegistrarDNSAnswers(response *miekgdns.Msg, question miekgdns.Question, useNAPTR bool) {
	name := strings.ToLower(question.Name)
	switch {
	case !useNAPTR && question.Qtype == miekgdns.TypeSRV && name == "_sip._tcp.ims.example.":
		response.Answer = append(response.Answer,
			netSRVHeader{target: "a.example.", port: 5060, priority: 10, weight: 10}.record(question.Name),
			netSRVHeader{target: "b.example.", port: 5070, priority: 10, weight: 20}.record(question.Name),
		)
	case useNAPTR && question.Qtype == miekgdns.TypeNAPTR && name == "naptr.example.":
		response.Answer = append(response.Answer, &miekgdns.NAPTR{
			Hdr:   miekgdns.RR_Header{Name: question.Name, Rrtype: miekgdns.TypeNAPTR, Class: miekgdns.ClassINET, Ttl: 60},
			Order: 10, Preference: 10, Service: "SIP+D2U", Replacement: "_sip._udp.alt.example.",
		})
	case useNAPTR && question.Qtype == miekgdns.TypeSRV && name == "_sip._udp.alt.example.":
		response.Answer = append(response.Answer,
			netSRVHeader{target: "c.example.", port: 5090}.record(question.Name),
		)
	case question.Qtype == miekgdns.TypeA:
		if address := testIPv4ForName(name); address != nil {
			response.Answer = append(response.Answer, &miekgdns.A{
				Hdr: addressHeader(question.Name, miekgdns.TypeA), A: address,
			})
		}
	case question.Qtype == miekgdns.TypeAAAA:
		if address := testIPv6ForName(name); address != nil {
			response.Answer = append(response.Answer, &miekgdns.AAAA{
				Hdr: addressHeader(question.Name, miekgdns.TypeAAAA), AAAA: address,
			})
		}
	}
}

type netSRVHeader struct {
	target   string
	port     uint16
	priority uint16
	weight   uint16
}

func (header netSRVHeader) record(name string) *miekgdns.SRV {
	return &miekgdns.SRV{
		Hdr: addressHeader(name, miekgdns.TypeSRV), Target: header.target,
		Port: header.port, Priority: header.priority, Weight: header.weight,
	}
}

func addressHeader(name string, recordType uint16) miekgdns.RR_Header {
	return miekgdns.RR_Header{Name: name, Rrtype: recordType, Class: miekgdns.ClassINET, Ttl: 60}
}

func testIPv4ForName(name string) net.IP {
	switch name {
	case "a.example.":
		return net.ParseIP("192.0.2.10")
	case "b.example.":
		return net.ParseIP("192.0.2.20")
	case "c.example.":
		return net.ParseIP("192.0.2.30")
	case "fallback.example.":
		return net.ParseIP("192.0.2.40")
	default:
		return nil
	}
}

func testIPv6ForName(name string) net.IP {
	switch name {
	case "a.example.":
		return net.ParseIP("2001:db8::10")
	case "b.example.":
		return net.ParseIP("2001:db8::20")
	case "c.example.":
		return net.ParseIP("2001:db8::30")
	case "fallback.example.":
		return net.ParseIP("2001:db8::40")
	default:
		return nil
	}
}
