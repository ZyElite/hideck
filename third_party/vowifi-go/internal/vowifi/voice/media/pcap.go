package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const (
	pcapSnapLength       = 65535
	pcapLinkUser0        = 147
	defaultPCAPDirectory = "pcap"
	pcapTimestampLayout  = "20060102_150405"
	currentPCAPLayout    = "20060102_150405.000"
)

type openedPCAP struct {
	writer packetCaptureWriter
	file   *os.File
	label  string
}

type pcapStartConfig struct {
	open              func() (openedPCAP, error)
	activeError       string
	headerErrorPrefix string
	includeCloseError bool
}

// StartPCAP preserves the original directory-based API and file semantics.
func (r *RTPRelay) StartPCAP(outputDir string) error {
	return r.startPCAP(pcapStartConfig{
		open:              func() (openedPCAP, error) { return r.openLegacyPCAP(outputDir) },
		activeError:       "PCAP 录制已在进行中",
		headerErrorPrefix: "写入 PCAP 头失败",
	})
}

// StartPCAPCurrent retains the additive path and injected-writer forms.
func (r *RTPRelay) StartPCAPCurrent(target any) error {
	return r.startPCAP(pcapStartConfig{
		open:              func() (openedPCAP, error) { return r.openCurrentPCAPTarget(target) },
		activeError:       "media: PCAP recording is already active",
		headerErrorPrefix: "media: write PCAP header",
		includeCloseError: true,
	})
}

func (r *RTPRelay) startPCAP(config pcapStartConfig) error {
	if r == nil {
		return errors.New("media: nil relay")
	}
	r.pcapMu.Lock()
	defer r.pcapMu.Unlock()
	if r.pcapWriter != nil || r.pcapFile != nil {
		return errors.New(config.activeError)
	}
	opened, err := config.open()
	if err != nil {
		return err
	}
	if err := writeAll(opened.writer, pcapGlobalHeader()); err != nil {
		closeErr := opened.writer.Close()
		if config.includeCloseError {
			err = errors.Join(err, closeErr)
		}
		return fmt.Errorf("%s: %w", config.headerErrorPrefix, err)
	}
	r.pcapWriter = opened.writer
	r.pcapFile = opened.file
	r.pcapEnable = true
	r.pcapErr = nil
	r.captureErr = nil
	if opened.file != nil {
		r.pcapPath = opened.label
	}
	deviceID, _ := r.logContext()
	logging.Debug("PCAP 录制已开始", "device", deviceID, "file", opened.label)
	return nil
}

func (r *RTPRelay) openLegacyPCAP(outputDir string) (openedPCAP, error) {
	deviceID, _ := r.logContext()
	path := legacyCapturePath(outputDir, deviceID, time.Now())
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o666)
	if err != nil {
		return openedPCAP{}, fmt.Errorf("创建 PCAP 文件失败: %w", err)
	}
	return openedPCAP{writer: file, file: file, label: path}, nil
}

func legacyCapturePath(outputDir, deviceID string, now time.Time) string {
	if outputDir == "" {
		outputDir = defaultPCAPDirectory
	}
	return fmt.Sprintf("%s/rtp_%s_%s.pcap", outputDir, deviceID, now.Format(pcapTimestampLayout))
}

func (r *RTPRelay) openCurrentPCAPTarget(target any) (openedPCAP, error) {
	switch value := target.(type) {
	case string:
		path, err := r.capturePath(value)
		if err != nil {
			return openedPCAP{}, err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o666)
		if err != nil {
			return openedPCAP{}, fmt.Errorf("media: create PCAP file: %w", err)
		}
		return openedPCAP{writer: file, file: file, label: path}, nil
	case packetCaptureWriter:
		if isNilCaptureWriter(value) {
			return openedPCAP{}, errors.New("media: PCAP writer is nil")
		}
		return openedPCAP{writer: value, label: fmt.Sprintf("%T", value)}, nil
	default:
		return openedPCAP{}, fmt.Errorf("media: unsupported PCAP target %T", target)
	}
}

func isNilCaptureWriter(writer packetCaptureWriter) bool {
	value := reflect.ValueOf(writer)
	if !value.IsValid() {
		return true
	}
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
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
	name := fmt.Sprintf("rtp_%s_%s.pcap", deviceID, time.Now().Format(currentPCAPLayout))
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

// StopPCAP preserves the original void cleanup API.
func (r *RTPRelay) StopPCAP() {
	_ = r.StopPCAPCurrent()
}

// StopPCAPCurrent closes the capture and exposes write and close failures.
func (r *RTPRelay) StopPCAPCurrent() error {
	if r == nil {
		return nil
	}
	r.pcapMu.Lock()
	defer r.pcapMu.Unlock()
	w := r.pcapWriter
	err := r.pcapErr
	recorder := r.audioRecorder
	wasActive := w != nil || r.pcapFile != nil || recorder != nil
	if w != nil {
		err = errors.Join(err, w.Close())
	}
	if recorder != nil {
		err = errors.Join(err, r.audioErr, recorder.close())
	}
	r.pcapWriter = nil
	r.pcapFile = nil
	r.pcapEnable = false
	r.pcapErr = nil
	r.audioRecorder = nil
	r.audioErr = nil
	r.captureErr = errors.Join(r.captureErr, err)
	if wasActive {
		deviceID, _ := r.logContext()
		logging.Debug("PCAP 录制已停止", "device", deviceID)
	}
	return err
}

// StartCallCapture starts packet capture and reserves the matching audio base path.
func (r *RTPRelay) StartCallCapture(basePath string) error {
	if r == nil {
		return errors.New("media: nil relay")
	}
	basePath = strings.TrimSpace(basePath)
	if basePath == "" {
		return errors.New("media: call capture path is empty")
	}
	r.pcapMu.Lock()
	r.audioTarget = basePath
	r.audioPath = ""
	r.audioCodec = ""
	r.captureErr = nil
	r.pcapMu.Unlock()
	if err := r.StartPCAPCurrent(basePath + ".pcap"); err != nil {
		r.pcapMu.Lock()
		r.audioTarget = ""
		r.pcapMu.Unlock()
		return err
	}
	return nil
}

// ConfigureAudioCapture opens the direct audio file for the negotiated codec.
func (r *RTPRelay) ConfigureAudioCapture(codecs []AudioCodec) error {
	if r == nil {
		return errors.New("media: nil relay")
	}
	r.pcapMu.Lock()
	defer r.pcapMu.Unlock()
	if r.audioTarget == "" {
		return nil
	}
	if r.audioRecorder != nil {
		return nil
	}
	recorder, err := newRTPAudioRecorder(r.audioTarget, codecs)
	if err != nil {
		r.captureErr = errors.Join(r.captureErr, err)
		return err
	}
	r.audioRecorder = recorder
	r.audioPath = recorder.path
	r.audioCodec = recorder.codec
	return nil
}

// CaptureSnapshot returns paths and retained recording errors after cleanup.
func (r *RTPRelay) CaptureSnapshot() CaptureSnapshot {
	if r == nil {
		return CaptureSnapshot{}
	}
	r.pcapMu.Lock()
	defer r.pcapMu.Unlock()
	return CaptureSnapshot{
		PCAPPath: r.pcapPath, AudioPath: r.audioPath, Codec: r.audioCodec,
		Err: errors.Join(r.captureErr, r.pcapErr, r.audioErr),
	}
}

func (r *RTPRelay) writeAudioPacket(packet []byte) {
	r.pcapMu.Lock()
	defer r.pcapMu.Unlock()
	if r.audioRecorder == nil || r.audioErr != nil {
		return
	}
	if err := r.audioRecorder.writeRTP(packet); err != nil {
		r.audioErr = fmt.Errorf("media: write audio recording: %w", err)
	}
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
		if written < 0 || written > len(data) {
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
