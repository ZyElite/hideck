package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	pcapSnapLength = 65535
	pcapLinkUser0  = 147
)

// StartPCAP accepts the original output directory/path and the additive
// writer form. A directory produces a unique per-device capture file.
func (r *RTPRelay) StartPCAP(target any) error {
	if r == nil {
		return errors.New("media: nil relay")
	}
	r.pcapMu.Lock()
	defer r.pcapMu.Unlock()
	if r.pcapWriter != nil || r.pcapFile != nil {
		return errors.New("media: PCAP recording is already active")
	}
	w, file, err := r.openPCAPTarget(target)
	if err != nil {
		return err
	}
	if err := writeAll(w, pcapGlobalHeader()); err != nil {
		_ = w.Close()
		return fmt.Errorf("media: write PCAP header: %w", err)
	}
	r.pcapWriter = w
	r.pcapFile = file
	r.pcapEnable = true
	r.pcapErr = nil
	return nil
}

func (r *RTPRelay) openPCAPTarget(target any) (packetCaptureWriter, *os.File, error) {
	switch value := target.(type) {
	case string:
		path, err := r.capturePath(value)
		if err != nil {
			return nil, nil, err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o666)
		if err != nil {
			return nil, nil, fmt.Errorf("media: create PCAP file: %w", err)
		}
		return file, file, nil
	case packetCaptureWriter:
		if value == nil {
			return nil, nil, errors.New("media: PCAP writer is nil")
		}
		return value, nil, nil
	default:
		return nil, nil, fmt.Errorf("media: unsupported PCAP target %T", target)
	}
}

func (r *RTPRelay) capturePath(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		target = "."
	}
	if strings.EqualFold(filepath.Ext(target), ".pcap") {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", fmt.Errorf("media: create PCAP directory: %w", err)
		}
		return target, nil
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", fmt.Errorf("media: create PCAP directory: %w", err)
	}
	deviceID, _ := r.logContext()
	deviceID = sanitizeCaptureName(deviceID)
	if deviceID == "" {
		deviceID = "voice"
	}
	name := fmt.Sprintf("rtp_%s_%s.pcap", deviceID, time.Now().Format("20060102_150405.000"))
	return filepath.Join(target, name), nil
}

func sanitizeCaptureName(value string) string {
	return strings.Map(func(char rune) rune {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9':
			return char
		case char == '-', char == '_':
			return char
		default:
			return '_'
		}
	}, strings.TrimSpace(value))
}

// StopPCAP closes the active capture and exposes any earlier write failure.
func (r *RTPRelay) StopPCAP() error {
	if r == nil {
		return nil
	}
	r.pcapMu.Lock()
	w := r.pcapWriter
	err := r.pcapErr
	r.pcapWriter = nil
	r.pcapFile = nil
	r.pcapEnable = false
	r.pcapErr = nil
	r.pcapMu.Unlock()
	if w != nil {
		err = errors.Join(err, w.Close())
	}
	return err
}

func (r *RTPRelay) writePCAPPacket(packet []byte, direction byte) {
	if r == nil {
		return
	}
	r.pcapMu.Lock()
	defer r.pcapMu.Unlock()
	if !r.pcapEnable || r.pcapWriter == nil || r.pcapErr != nil {
		return
	}
	recordLength := len(packet) + 1
	if err := writeAll(r.pcapWriter, pcapPacketHeader(recordLength)); err != nil {
		r.pcapErr = fmt.Errorf("media: write PCAP packet header: %w", err)
		return
	}
	if err := writeAll(r.pcapWriter, []byte{direction}); err != nil {
		r.pcapErr = fmt.Errorf("media: write PCAP direction: %w", err)
		return
	}
	if err := writeAll(r.pcapWriter, packet); err != nil {
		r.pcapErr = fmt.Errorf("media: write PCAP packet: %w", err)
	}
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func pcapGlobalHeader() []byte {
	header := make([]byte, 24)
	binary.LittleEndian.PutUint32(header[0:4], 0xa1b2c3d4)
	binary.LittleEndian.PutUint16(header[4:6], 2)
	binary.LittleEndian.PutUint16(header[6:8], 4)
	binary.LittleEndian.PutUint32(header[16:20], pcapSnapLength)
	binary.LittleEndian.PutUint32(header[20:24], pcapLinkUser0)
	return header
}

func pcapPacketHeader(length int) []byte {
	header := make([]byte, 16)
	now := time.Now()
	binary.LittleEndian.PutUint32(header[0:4], uint32(now.Unix()))
	binary.LittleEndian.PutUint32(header[4:8], uint32(now.Nanosecond()/1000))
	binary.LittleEndian.PutUint32(header[8:12], uint32(length))
	binary.LittleEndian.PutUint32(header[12:16], uint32(length))
	return header
}
