package ipsec

import (
	"encoding/binary"
	"net"
	"testing"
)

func TestResolveUDPAddrAllUsesConfiguredDNSServer(t *testing.T) {
	server := newTestDNSServer(t, net.IPv4(192, 0, 2, 25))
	defer server.Close()
	preferred, candidates, err := ResolveUDPAddrAll("epdg.test:500", server.LocalAddr().String())
	if err != nil {
		t.Fatalf("ResolveUDPAddrAll: %v", err)
	}
	want := net.IPv4(192, 0, 2, 25)
	if !preferred.IP.Equal(want) || preferred.Port != 500 {
		t.Fatalf("preferred endpoint = %v, want %v:500", preferred, want)
	}
	if len(candidates) != 1 || !candidates[0].Equal(want) {
		t.Fatalf("candidates = %v, want [%v]", candidates, want)
	}
}

func TestResolveUDPAddrAllRejectsInvalidCustomDNS(t *testing.T) {
	if _, _, err := ResolveUDPAddrAll("epdg.test:500", "not a DNS address"); err == nil {
		t.Fatal("invalid custom DNS address was accepted")
	}
}

type testDNSServer struct {
	*net.UDPConn
	answer net.IP
}

func newTestDNSServer(t *testing.T, answer net.IP) *testDNSServer {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	server := &testDNSServer{UDPConn: conn, answer: answer.To4()}
	go server.serve()
	return server
}

func (s *testDNSServer) serve() {
	buffer := make([]byte, 1500)
	for {
		length, source, err := s.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		response := s.response(buffer[:length])
		if response != nil {
			_, _ = s.WriteToUDP(response, source)
		}
	}
}

func (s *testDNSServer) response(query []byte) []byte {
	questionEnd, ok := dnsQuestionEnd(query)
	if !ok {
		return nil
	}
	questionType := binary.BigEndian.Uint16(query[questionEnd-4 : questionEnd-2])
	answerCount := uint16(0)
	if questionType == 1 {
		answerCount = 1
	}
	response := make([]byte, 12, 32+questionEnd)
	copy(response[:2], query[:2])
	binary.BigEndian.PutUint16(response[2:4], 0x8180)
	binary.BigEndian.PutUint16(response[4:6], 1)
	binary.BigEndian.PutUint16(response[6:8], answerCount)
	response = append(response, query[12:questionEnd]...)
	if answerCount == 0 {
		return response
	}
	response = append(response, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 0, 0, 4)
	return append(response, s.answer...)
}

func dnsQuestionEnd(query []byte) (int, bool) {
	if len(query) < 17 {
		return 0, false
	}
	index := 12
	for index < len(query) && query[index] != 0 {
		labelLength := int(query[index])
		index += labelLength + 1
	}
	index++
	if index+4 > len(query) {
		return 0, false
	}
	return index + 4, true
}
