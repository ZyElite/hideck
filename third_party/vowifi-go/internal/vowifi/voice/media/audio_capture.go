package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	rtpFixedHeaderLength = 12
	wavHeaderLength      = 44
)

var (
	amrFrameBytes   = [...]int{12, 13, 15, 17, 19, 20, 26, 31, 5, 0, 0, 0, 0, 0, 0, 0}
	amrWBFrameBytes = [...]int{17, 23, 32, 36, 40, 46, 50, 58, 60, 5, 0, 0, 0, 0, 0, 0}
)

type rtpAudioRecorder struct {
	file        *os.File
	path        string
	codec       string
	payloadType int
	clockRate   int
	dataBytes   uint32
	frames      uint64
}

func newRTPAudioRecorder(target string, codecs []AudioCodec) (*rtpAudioRecorder, error) {
	codec, extension, err := selectRecordableCodec(codecs)
	if err != nil {
		return nil, err
	}
	path := target + extension
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("media: create audio capture directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("media: create audio capture: %w", err)
	}
	recorder := &rtpAudioRecorder{
		file: file, path: path, codec: strings.ToUpper(codec.Name),
		payloadType: codec.PayloadType, clockRate: codec.ClockRate,
	}
	if err := recorder.writeHeader(); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return recorder, nil
}

func selectRecordableCodec(codecs []AudioCodec) (AudioCodec, string, error) {
	for _, codec := range codecs {
		switch strings.ToUpper(strings.TrimSpace(codec.Name)) {
		case "AMR-WB":
			if !hasOctetAlignedFMTP(codec.Fmtp) {
				continue
			}
			return codec, ".amr-wb", nil
		case "AMR":
			if !hasOctetAlignedFMTP(codec.Fmtp) {
				continue
			}
			return codec, ".amr", nil
		case "PCMU", "PCMA":
			if codec.ClockRate == 0 {
				codec.ClockRate = 8000
			}
			return codec, ".wav", nil
		}
	}
	return AudioCodec{}, "", errors.New("media: negotiated RTP codec cannot be recorded")
}

func hasOctetAlignedFMTP(fmtp string) bool {
	for _, field := range strings.FieldsFunc(strings.ToLower(fmtp), func(char rune) bool {
		return char == ';' || char == ' ' || char == '\t'
	}) {
		if strings.TrimSpace(field) == "octet-align=1" {
			return true
		}
	}
	return false
}

func (r *rtpAudioRecorder) writeHeader() error {
	switch r.codec {
	case "AMR-WB":
		_, err := r.file.WriteString("#!AMR-WB\n")
		return err
	case "AMR":
		_, err := r.file.WriteString("#!AMR\n")
		return err
	case "PCMU", "PCMA":
		_, err := r.file.Write(make([]byte, wavHeaderLength))
		return err
	default:
		return fmt.Errorf("media: unsupported audio codec %q", r.codec)
	}
}

func (r *rtpAudioRecorder) writeRTP(packet []byte) error {
	payloadType, payload, err := parseRTPPayload(packet)
	if err != nil || payloadType != r.payloadType {
		return err
	}
	switch r.codec {
	case "AMR-WB":
		return r.writeOctetAlignedAMR(payload, amrWBFrameBytes[:])
	case "AMR":
		return r.writeOctetAlignedAMR(payload, amrFrameBytes[:])
	case "PCMU", "PCMA":
		return r.writeG711(payload)
	default:
		return fmt.Errorf("media: unsupported audio codec %q", r.codec)
	}
}

func parseRTPPayload(packet []byte) (int, []byte, error) {
	if len(packet) < rtpFixedHeaderLength || packet[0]>>6 != 2 {
		return 0, nil, errors.New("media: malformed RTP packet")
	}
	offset := rtpFixedHeaderLength + int(packet[0]&0x0f)*4
	if packet[0]&0x10 != 0 {
		if len(packet) < offset+4 {
			return 0, nil, errors.New("media: truncated RTP extension")
		}
		offset += 4 + int(binary.BigEndian.Uint16(packet[offset+2:offset+4]))*4
	}
	padding := 0
	if packet[0]&0x20 != 0 {
		padding = int(packet[len(packet)-1])
	}
	if offset > len(packet)-padding || padding < 0 {
		return 0, nil, errors.New("media: invalid RTP payload bounds")
	}
	return int(packet[1] & 0x7f), packet[offset : len(packet)-padding], nil
}

func (r *rtpAudioRecorder) writeOctetAlignedAMR(payload []byte, sizes []int) error {
	if len(payload) < 2 {
		return errors.New("media: truncated AMR RTP payload")
	}
	index := 1 // CMR octet
	var toc []byte
	for {
		if index >= len(payload) {
			return errors.New("media: truncated AMR table of contents")
		}
		entry := payload[index]
		toc = append(toc, entry)
		index++
		if entry&0x80 == 0 {
			break
		}
	}
	for _, entry := range toc {
		frameType := int((entry >> 3) & 0x0f)
		frameBytes := sizes[frameType]
		if index+frameBytes > len(payload) {
			return errors.New("media: truncated AMR speech frame")
		}
		if _, err := r.file.Write([]byte{entry & 0x7c}); err != nil {
			return err
		}
		if frameBytes > 0 {
			if _, err := r.file.Write(payload[index : index+frameBytes]); err != nil {
				return err
			}
		}
		index += frameBytes
		r.frames++
	}
	return nil
}

func (r *rtpAudioRecorder) writeG711(payload []byte) error {
	pcm := make([]byte, len(payload)*2)
	for index, sample := range payload {
		value := decodeMuLaw(sample)
		if r.codec == "PCMA" {
			value = decodeALaw(sample)
		}
		binary.LittleEndian.PutUint16(pcm[index*2:], uint16(value))
	}
	written, err := r.file.Write(pcm)
	r.dataBytes += uint32(written)
	if written > 0 {
		r.frames++
	}
	return err
}

func (r *rtpAudioRecorder) close() error {
	if r == nil || r.file == nil {
		return nil
	}
	var err error
	if r.codec == "PCMU" || r.codec == "PCMA" {
		err = r.finalizeWAV()
	}
	if r.frames == 0 {
		err = errors.Join(err, errors.New("media: recording contains no audio frames"))
	}
	err = errors.Join(err, r.file.Close())
	r.file = nil
	return err
}

func (r *rtpAudioRecorder) finalizeWAV() error {
	header := make([]byte, wavHeaderLength)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], 36+r.dataBytes)
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], uint32(r.clockRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(r.clockRate*2))
	binary.LittleEndian.PutUint16(header[32:34], 2)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], r.dataBytes)
	if _, err := r.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err := r.file.Write(header)
	return err
}

func decodeMuLaw(value byte) int16 {
	value = ^value
	magnitude := ((int(value&0x0f) << 3) + 0x84) << ((value & 0x70) >> 4)
	if value&0x80 != 0 {
		return int16(0x84 - magnitude)
	}
	return int16(magnitude - 0x84)
}

func decodeALaw(value byte) int16 {
	value ^= 0x55
	magnitude := int(value&0x0f) << 4
	exponent := int((value & 0x70) >> 4)
	if exponent == 0 {
		magnitude += 8
	} else {
		magnitude = (magnitude + 0x108) << (exponent - 1)
	}
	if value&0x80 == 0 {
		magnitude = -magnitude
	}
	return int16(magnitude)
}
