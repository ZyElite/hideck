package imscore

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/emiago/sipgo/sip"
)

func TestSIPParserAcceptsBuiltRegisterAndResponse(t *testing.T) {
	service := &Service{cfg: &IMSConfig{
		IMSI: "234102356143376", IMPI: "234102356143376@ims.example",
		IMPU: "sip:234102356143376@ims.example", Domain: "ims.example",
		LocalIP: net.IPv4(192, 0, 2, 10), Transport: "tcp",
	}}
	request := service.buildRegister(&registerSession{
		callID: "parser-call", fromTag: "parser-tag", cseq: 1,
	}, "")
	parsedRequest, err := readSIPStreamMessage(bufio.NewReader(stringsReader(request)))
	if err != nil {
		t.Fatalf("parse built REGISTER: %v\n%s", err, request)
	}
	response := registerWireResponse(parsedRequest, 401, digestChallengeHeaderNoQOP())
	decoder := newSIPStreamDecoder(stringsReader(response))
	defer decoder.Close()
	if _, err := decoder.ReadMessage(); err != nil {
		t.Fatalf("parse REGISTER response: %v\n%s", err, response)
	}
}

func stringsReader(value string) *bytes.Reader {
	return bytes.NewReader([]byte(value))
}

func TestUnfoldSIPHeadersMatchesRecoveredWireBehavior(t *testing.T) {
	raw := []byte("X-One: first\r\n \tsecond\r\nX-Two: left\n\tright\r\n\r\n")
	want := "X-One: first second\r\nX-Two: left right\r\n\r\n"
	if got := string(unfoldSIPHeaders(raw)); got != want {
		t.Fatalf("unfolded headers = %q, want %q", got, want)
	}
	if unfolded := unfoldSIPHeaders(nil); unfolded != nil {
		t.Fatalf("nil headers unfolded to %#v", unfolded)
	}
}

func TestParseSIPResponsePreservesStructuredHeaders(t *testing.T) {
	wire := "SIP/2.0 200 OK\r\n" +
		"v: SIP/2.0/TCP 192.0.2.1:5060;branch=z9hG4bK-parser\r\n" +
		"i: parser-call\r\nCSeq: 7 REGISTER\r\n" +
		"Contact: <sip:first@ims.example>\r\n" +
		"Contact: <sip:second@ims.example>;expires=42\r\n" +
		"X-Folded: first\r\n\tsecond\r\nl: 4\r\n\r\nbody"
	response, err := parseSIPResponse(wire)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || response.CallID != "parser-call" || response.CSeq != "7 REGISTER" {
		t.Fatalf("parsed response = %#v", response)
	}
	if got := response.Header("Via"); !strings.Contains(got, "z9hG4bK-parser") {
		t.Fatalf("compact Via = %q", got)
	}
	if got := response.Header("X-Folded"); got != "first second" {
		t.Fatalf("folded header = %q", got)
	}
	if values := response.HeaderValues("Contact"); len(values) != 2 {
		t.Fatalf("Contact values = %#v", values)
	}
	if contacts := response.Headers["Contact"]; !strings.Contains(contacts, "first@ims.example") ||
		!strings.Contains(contacts, "second@ims.example") {
		t.Fatalf("projected Contact header = %q", contacts)
	}
	if expires := parseRegisterExpiresFromResponse(response, 600); expires != 42 {
		t.Fatalf("registration expires = %d", expires)
	}
	if string(response.Body) != "body" {
		t.Fatalf("response body = %q", response.Body)
	}
}

func TestParseSIPResponseRejectsRequestAndMalformedStatus(t *testing.T) {
	request := "OPTIONS sip:user@ims.example SIP/2.0\r\nContent-Length: 0\r\n\r\n"
	if _, err := parseSIPResponse(request); !errors.Is(err, errExpectedSIPResponse) {
		t.Fatalf("request parse error = %v", err)
	}
	malformed := "SIP/2.0 nope OK\r\nContent-Length: 0\r\n\r\n"
	if _, err := parseSIPResponse(malformed); err == nil {
		t.Fatal("malformed response was accepted")
	}
}

func TestSIPStreamDecoderHandlesFragmentationAndPipelining(t *testing.T) {
	chunks := &chunkReader{chunks: [][]byte{
		[]byte("\r\nSIP/2.0 200 OK\r\nVia: SIP/2.0/TCP host;branch=z9hG4bK-one\r\n"),
		[]byte("Call-ID: one\r\nCSeq: 1 OPTIONS\r\nContent-Length: 0\r\n\r\nOPT"),
		[]byte("IONS sip:user@ims.example SIP/2.0\r\nVia: SIP/2.0/TCP host;branch=z9hG4bK-two\r\n"),
		[]byte("Call-ID: two\r\nCSeq: 2 OPTIONS\r\nContent-Length: 0\r\n\r\n"),
	}}
	decoder := newSIPStreamDecoder(chunks)
	defer decoder.Close()
	first, err := decoder.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if response, ok := first.(*sip.Response); !ok || response.StatusCode != 200 {
		t.Fatalf("first message = %T %#v", first, first)
	}
	second, err := decoder.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if request, ok := second.(*sip.Request); !ok || request.Method != sip.OPTIONS {
		t.Fatalf("second message = %T %#v", second, second)
	}
	if _, err := decoder.ReadMessage(); !errors.Is(err, io.EOF) {
		t.Fatalf("decoder terminal error = %v", err)
	}
}

func TestReadSIPResponseUsesRecoveredTypeCheck(t *testing.T) {
	responseWire := "SIP/2.0 486 Busy Here\r\nContent-Length: 0\r\n\r\n"
	response, err := readSIPResponse(stringsReader(responseWire))
	if err != nil || response.StatusCode != 486 {
		t.Fatalf("read response = %#v, %v", response, err)
	}
	requestWire := "OPTIONS sip:user@ims.example SIP/2.0\r\nContent-Length: 0\r\n\r\n"
	if _, err := readSIPResponse(stringsReader(requestWire)); !errors.Is(err, errExpectedSIPResponse) {
		t.Fatalf("request type error = %v", err)
	}
}

func TestReadSIPStreamMessageRequiresReliableLength(t *testing.T) {
	missingLength := "OPTIONS sip:user@ims.example SIP/2.0\r\nCall-ID: missing\r\n\r\n"
	if _, err := readSIPStreamMessage(bufio.NewReader(stringsReader(missingLength))); !errors.Is(err, sip.ErrParseReadBodyIncomplete) {
		t.Fatalf("missing Content-Length error = %v", err)
	}
	wire := "\r\nOPTIONS sip:user@ims.example SIP/2.0\r\ni: compact\r\nCSeq: 1 OPTIONS\r\nl: 0\r\n\r\n"
	parsed, err := readSIPStreamMessage(bufio.NewReader(stringsReader(wire)))
	if err != nil || rawSIPHeaderValue(parsed, "Call-ID") != "compact" {
		t.Fatalf("compact stream parse = %q, %v", parsed, err)
	}
	tooLarge := "OPTIONS sip:user@ims.example SIP/2.0\r\nContent-Length: 65535\r\n\r\n"
	if _, err := readSIPStreamMessage(bufio.NewReader(stringsReader(tooLarge))); !errors.Is(err, sip.ErrMessageTooLarge) {
		t.Fatalf("oversized stream error = %v", err)
	}
}

func TestDispatchInboundSIPUsesStructuredCompactHeaders(t *testing.T) {
	service := &Service{transport: newSIPTransport()}
	wire := "OPTIONS sip:user@ims.example SIP/2.0\r\n" +
		"v: SIP/2.0/UDP 192.0.2.1:5060;branch=z9hG4bK-compact\r\n" +
		"f: <sip:peer@ims.example>;tag=remote\r\nt: <sip:user@ims.example>\r\n" +
		"i: compact-call\r\nCSeq: 9 OPTIONS\r\nl: 0\r\n\r\n"
	var response string
	if err := service.dispatchInboundSIP(wire, func(value string) error {
		response = value
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"SIP/2.0 200 OK", "Via: SIP/2.0/UDP 192.0.2.1:5060;branch=z9hG4bK-compact",
		"Call-ID: compact-call", "CSeq: 9 OPTIONS",
	} {
		if !strings.Contains(response, expected) {
			t.Fatalf("response omitted %q:\n%s", expected, response)
		}
	}
	select {
	case delivered := <-service.transport.Requests():
		if rawSIPHeaderValue(delivered, "Call-ID") != "compact-call" {
			t.Fatalf("delivered request = %q", delivered)
		}
	default:
		t.Fatal("valid request was not delivered")
	}
}

func TestDispatchInboundSIPRejectsMalformedWireBeforeDelivery(t *testing.T) {
	service := &Service{transport: newSIPTransport()}
	replied := false
	err := service.dispatchInboundSIP("OPTIONS sip:user@ims.example SIP/2.0\n\n", func(string) error {
		replied = true
		return nil
	})
	if err == nil || replied {
		t.Fatalf("malformed dispatch error = %v, replied = %v", err, replied)
	}
	select {
	case request := <-service.transport.Requests():
		t.Fatalf("malformed request delivered: %q", request)
	default:
	}
}

func TestParseInboundRPDURecoveredClassification(t *testing.T) {
	if err := parseInboundRPDU([]byte{0x03}); err != nil {
		t.Fatalf("RP-ACK parse: %v", err)
	}
	if err := parseInboundRPDU([]byte{0x01, 0x44}); err == nil {
		t.Fatal("malformed RP-DATA was accepted")
	}
	if err := parseInboundRPDU([]byte{0x05, 0x44, 0x00}); err == nil {
		t.Fatal("malformed RP-ERROR was accepted")
	}
	if err := parseInboundRPDU([]byte{0xff}); err == nil || err.Error() != "unsupported rpdu mti=0xff" {
		t.Fatalf("unsupported RPDU error = %v", err)
	}
}

type chunkReader struct {
	chunks [][]byte
}

func (r *chunkReader) Read(buffer []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	chunk := r.chunks[0]
	r.chunks = r.chunks[1:]
	read := copy(buffer, chunk)
	if read < len(chunk) {
		r.chunks = append([][]byte{chunk[read:]}, r.chunks...)
	}
	return read, nil
}
