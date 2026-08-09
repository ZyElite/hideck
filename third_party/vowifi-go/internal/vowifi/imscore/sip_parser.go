package imscore

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/smscodec"
	"github.com/iniwex5/vowifi-go/internal/vowifi/sipkit"
)

const sipParserReadBufferSize = 8 * 1024

var errExpectedSIPResponse = errors.New("expected response but got request")

type sipResponse struct {
	StatusCode int
	Reason     string
	CallID     string
	CSeq       string
	Headers    map[string]string
	Body       []byte
	parsed     *sip.Response
}

func (r *sipResponse) Header(name string) string {
	values := r.HeaderValues(name)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (r *sipResponse) HeaderValues(name string) []string {
	if r == nil {
		return nil
	}
	if r.parsed != nil {
		headers := r.parsed.GetHeaders(name)
		values := make([]string, 0, len(headers))
		for _, header := range headers {
			values = append(values, sipkit.HeaderValue(header, true))
		}
		return values
	}
	for key, value := range r.Headers {
		if strings.EqualFold(key, name) {
			return []string{value}
		}
	}
	return nil
}

func unfoldSIPHeaders(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	unfolded := make([]byte, 0, len(data))
	for offset := 0; offset < len(data); {
		next, continuation := sipHeaderContinuation(data, offset)
		if !continuation {
			unfolded = append(unfolded, data[offset])
			offset++
			continue
		}
		unfolded = append(unfolded, ' ')
		offset = next
		for offset < len(data) && (data[offset] == ' ' || data[offset] == '\t') {
			offset++
		}
	}
	return unfolded
}

func sipHeaderContinuation(data []byte, offset int) (int, bool) {
	if offset+2 < len(data) && data[offset] == '\r' && data[offset+1] == '\n' &&
		(data[offset+2] == ' ' || data[offset+2] == '\t') {
		return offset + 3, true
	}
	if offset+1 < len(data) && data[offset] == '\n' &&
		(data[offset+1] == ' ' || data[offset+1] == '\t') {
		return offset + 2, true
	}
	return offset, false
}

func parseSIPMessage(raw string) (sip.Message, error) {
	return sip.NewParser().ParseSIP(unfoldSIPHeaders([]byte(raw)))
}

func parseSIPResponse(raw string) (*sipResponse, error) {
	message, err := parseSIPMessage(raw)
	if err != nil {
		return nil, err
	}
	response, ok := message.(*sip.Response)
	if !ok {
		return nil, errExpectedSIPResponse
	}
	return newSIPResponse(response), nil
}

func newSIPResponse(response *sip.Response) *sipResponse {
	if response == nil {
		return nil
	}
	result := &sipResponse{
		StatusCode: response.StatusCode,
		Reason:     response.Reason,
		Headers:    make(map[string]string),
		Body:       append([]byte(nil), response.Body()...),
		parsed:     response,
	}
	for _, header := range response.Headers() {
		value := sipkit.HeaderValue(header, true)
		if previous, exists := result.Headers[header.Name()]; exists {
			result.Headers[header.Name()] = previous + ", " + value
		} else {
			result.Headers[header.Name()] = value
		}
	}
	result.CallID = sipkit.FirstHeaderValue(response, "Call-ID", true)
	result.CSeq = sipkit.FirstHeaderValue(response, "CSeq", true)
	return result
}

type sipStreamDecoder struct {
	reader  io.Reader
	stream  *sip.ParserStream
	pending error
	buffer  [sipParserReadBufferSize]byte
}

func newSIPStreamDecoder(reader io.Reader) *sipStreamDecoder {
	return &sipStreamDecoder{reader: reader, stream: sip.NewParser().NewSIPStream()}
}

func (d *sipStreamDecoder) Close() {
	if d != nil && d.stream != nil {
		d.stream.Close()
	}
}

func (d *sipStreamDecoder) ReadMessage() (sip.Message, error) {
	if d == nil || d.reader == nil || d.stream == nil {
		return nil, errors.New("imscore: nil SIP stream decoder")
	}
	for {
		message, _, err := d.stream.ParseNext()
		if err == nil {
			return message, nil
		}
		if !isPartialSIPParseError(err) {
			return nil, err
		}
		if d.pending != nil {
			return nil, d.pending
		}
		read, readErr := d.reader.Read(d.buffer[:])
		if read > 0 {
			_, _ = d.stream.Write(unfoldSIPHeaders(d.buffer[:read]))
		}
		if readErr != nil {
			d.pending = readErr
		}
		if read == 0 && readErr == nil {
			return nil, io.ErrNoProgress
		}
	}
}

func isPartialSIPParseError(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, sip.ErrParseSipPartial) ||
		errors.Is(err, sip.ErrParseReadBodyIncomplete)
}

func readSIPResponse(reader io.Reader) (*sip.Response, error) {
	decoder := newSIPStreamDecoder(reader)
	defer decoder.Close()
	message, err := decoder.ReadMessage()
	if err != nil {
		return nil, err
	}
	response, ok := message.(*sip.Response)
	if !ok {
		return nil, errExpectedSIPResponse
	}
	return response, nil
}

func readSIPStreamMessage(reader *bufio.Reader) (string, error) {
	if reader == nil {
		return "", errors.New("imscore: nil SIP stream reader")
	}
	header, err := readSIPStreamHeader(reader)
	if err != nil {
		return "", err
	}
	unfolded := unfoldSIPHeaders(header)
	message, _, err := sip.NewParser().ParseHeaders(unfolded, true)
	if err != nil {
		return "", err
	}
	contentLength := message.ContentLength()
	if contentLength == nil {
		return "", sip.ErrParseReadBodyIncomplete
	}
	bodyLength := int64(*contentLength)
	if bodyLength > int64(sip.ParseMaxMessageLength-len(unfolded)) {
		return "", sip.ErrMessageTooLarge
	}
	body := make([]byte, int(bodyLength))
	if _, err := io.ReadFull(reader, body); err != nil {
		return "", err
	}
	wire := append(unfolded, body...)
	if _, err := sip.NewParser().ParseSIP(wire); err != nil {
		return "", err
	}
	return string(wire), nil
}

func readSIPStreamHeader(reader *bufio.Reader) ([]byte, error) {
	var header []byte
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		if len(line) < 2 || line[len(line)-2] != '\r' {
			return nil, sip.ErrParseLineNoCRLF
		}
		if len(header) == 0 && len(line) == 2 {
			continue
		}
		header = append(header, line...)
		if len(header) > sip.ParseMaxMessageLength {
			return nil, sip.ErrMessageTooLarge
		}
		if len(line) == 2 {
			return header, nil
		}
	}
}

func sipRequestMethod(raw string) string {
	message, err := parseSIPMessage(raw)
	if err != nil {
		return ""
	}
	request, ok := message.(*sip.Request)
	if !ok {
		return ""
	}
	return request.Method.String()
}

func rawSIPHeaderValue(raw, name string) string {
	message, err := parseSIPMessage(raw)
	if err != nil {
		return ""
	}
	return sipkit.FirstHeaderValue(message, name, true)
}

func rawSIPBody(raw string) ([]byte, error) {
	message, err := parseSIPMessage(raw)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), message.Body()...), nil
}

func parseInboundRPDU(body []byte) error {
	info := smscodec.ClassifyRPDU(body)
	switch info.Kind {
	case smscodec.RPDUKindAck:
		return nil
	case smscodec.RPDUKindData:
		_, _, _, _, err := smscodec.ParseRPDataWithAddresses(body)
		return err
	case smscodec.RPDUKindError:
		_, err := smscodec.ParseRPErrorCause(body)
		return err
	default:
		return fmt.Errorf("unsupported rpdu mti=0x%02x", info.RawType)
	}
}
