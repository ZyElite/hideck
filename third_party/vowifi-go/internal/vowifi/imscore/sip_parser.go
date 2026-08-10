package imscore

import (
	"bufio"
	"bytes"
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
	headerEnd := bytes.Index(data, []byte("\r\n\r\n"))
	if headerEnd < 0 {
		return unfoldSIPHeaderSection(data)
	}
	headerEnd += len("\r\n\r\n")
	header := unfoldSIPHeaderSection(data[:headerEnd])
	message := make([]byte, 0, len(header)+len(data)-headerEnd)
	message = append(message, header...)
	return append(message, data[headerEnd:]...)
}

func unfoldSIPHeaderSection(data []byte) []byte {
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
	reader    *bufio.Reader
	keepalive io.Writer
}

func newSIPStreamDecoder(reader io.Reader) *sipStreamDecoder {
	decoder := &sipStreamDecoder{reader: bufio.NewReaderSize(reader, sipParserReadBufferSize)}
	if writer, ok := reader.(io.Writer); ok {
		decoder.keepalive = writer
	}
	return decoder
}

func (d *sipStreamDecoder) Close() {}

func (d *sipStreamDecoder) ReadMessage() (sip.Message, error) {
	if d == nil || d.reader == nil {
		return nil, errors.New("imscore: nil SIP stream decoder")
	}
	message, _, err := readSIPStreamFrame(d.reader, d.keepalive)
	return message, err
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
	return readSIPStreamMessageWithKeepalive(reader, nil)
}

func readSIPStreamMessageWithKeepalive(reader *bufio.Reader, keepalive io.Writer) (string, error) {
	_, wire, err := readSIPStreamFrame(reader, keepalive)
	return wire, err
}

func readSIPStreamFrame(reader *bufio.Reader, keepalive io.Writer) (sip.Message, string, error) {
	if reader == nil {
		return nil, "", errors.New("imscore: nil SIP stream reader")
	}
	header, err := readSIPStreamHeaderWithKeepalive(reader, keepalive)
	if err != nil {
		return nil, "", err
	}
	unfolded := unfoldSIPHeaders(header)
	message, _, err := sip.NewParser().ParseHeaders(unfolded, true)
	if err != nil {
		if errors.Is(err, sip.ErrParseReadBodyIncomplete) {
			return nil, "", fmt.Errorf("%w: %s", err, summarizeSIPHeaderFrame(unfolded))
		}
		return nil, "", err
	}
	contentLength := message.ContentLength()
	if contentLength == nil {
		return nil, "", fmt.Errorf("%w: %s", sip.ErrParseReadBodyIncomplete,
			summarizeSIPHeaderFrame(unfolded))
	}
	bodyLength := int64(*contentLength)
	if bodyLength > int64(sip.ParseMaxMessageLength-len(unfolded)) {
		return nil, "", sip.ErrMessageTooLarge
	}
	body := make([]byte, int(bodyLength))
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, "", err
	}
	wire := make([]byte, 0, len(unfolded)+len(body))
	wire = append(wire, unfolded...)
	wire = append(wire, body...)
	message.SetBody(body)
	return message, string(wire), nil
}

func summarizeSIPHeaderFrame(header []byte) string {
	lines := strings.Split(string(header), "\r\n")
	startFields := strings.Fields(lines[0])
	startToken := "unknown"
	if len(startFields) > 0 {
		startToken = startFields[0]
	}
	names := make([]string, 0, len(lines))
	for _, line := range lines[1:] {
		name, _, found := strings.Cut(line, ":")
		if found {
			names = append(names, strings.TrimSpace(name))
		}
	}
	return fmt.Sprintf("start_token=%q headers=%s", startToken, strings.Join(names, ","))
}

func readSIPStreamHeader(reader *bufio.Reader) ([]byte, error) {
	return readSIPStreamHeaderWithKeepalive(reader, nil)
}

func readSIPStreamHeaderWithKeepalive(reader *bufio.Reader, keepalive io.Writer) ([]byte, error) {
	var header []byte
	blankLines := 0
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		if len(line) < 2 || line[len(line)-2] != '\r' {
			return nil, sip.ErrParseLineNoCRLF
		}
		if len(header) == 0 && len(line) == 2 {
			blankLines++
			if blankLines == 2 && keepalive != nil {
				if _, err := io.WriteString(keepalive, "\r\n"); err != nil {
					return nil, fmt.Errorf("imscore: write SIP keepalive pong: %w", err)
				}
			}
			if blankLines == 2 {
				blankLines = 0
			}
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
