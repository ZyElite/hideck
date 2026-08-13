package phone

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	mixedWAVHeaderBytes  = 44
	recordingFrameBuffer = 32
	mixedSampleRate      = 8000
)

type mixDirection uint8

const (
	mixToIMS mixDirection = iota
	mixFromIMS
)

type mixedRecorder struct {
	path      string
	file      *os.File
	toIMS     chan []int16
	fromIMS   chan []int16
	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
	lifecycle sync.RWMutex
	closed    bool
	resultMu  sync.Mutex
	resultErr error
	dataBytes uint32
}

type mixedFrameQueues struct {
	toIMS   [][]int16
	fromIMS [][]int16
	started bool
}

func newMixedRecorder(path string) (*mixedRecorder, error) {
	if path == "" {
		return nil, errors.New("phone: mixed recording path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("phone: create mixed recording directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("phone: create mixed recording: %w", err)
	}
	if _, err := file.Write(make([]byte, mixedWAVHeaderBytes)); err != nil {
		return nil, errors.Join(fmt.Errorf("phone: initialize mixed recording: %w", err), file.Close())
	}
	recorder := &mixedRecorder{
		path: path, file: file, toIMS: make(chan []int16, recordingFrameBuffer),
		fromIMS: make(chan []int16, recordingFrameBuffer), done: make(chan struct{}),
	}
	recorder.wg.Add(1)
	go recorder.run()
	return recorder, nil
}

func (recorder *mixedRecorder) Add(direction mixDirection, pcmu []byte) {
	if recorder == nil || len(pcmu) != browserSamplesPerFrame {
		return
	}
	recorder.lifecycle.RLock()
	defer recorder.lifecycle.RUnlock()
	if recorder.closed {
		return
	}
	frame := decodePCMU(pcmu)
	destination := recorder.toIMS
	if direction == mixFromIMS {
		destination = recorder.fromIMS
	}
	select {
	case destination <- frame:
	default:
		recorder.addError(errors.New("phone: mixed recording frame buffer overflow"))
	}
}

func (recorder *mixedRecorder) Close() error {
	if recorder == nil {
		return nil
	}
	recorder.closeOnce.Do(func() {
		recorder.lifecycle.Lock()
		recorder.closed = true
		close(recorder.done)
		recorder.lifecycle.Unlock()
		recorder.wg.Wait()
	})
	recorder.resultMu.Lock()
	defer recorder.resultMu.Unlock()
	return recorder.resultErr
}

func (recorder *mixedRecorder) run() {
	defer recorder.wg.Done()
	ticker := time.NewTicker(jitterTick)
	defer ticker.Stop()
	queues := &mixedFrameQueues{}
	for {
		select {
		case frame := <-recorder.toIMS:
			queues.toIMS = append(queues.toIMS, frame)
		case frame := <-recorder.fromIMS:
			queues.fromIMS = append(queues.fromIMS, frame)
		case <-ticker.C:
			recorder.writeNextFrame(queues)
		case <-recorder.done:
			recorder.drainFrames(queues)
			recorder.finalize()
			return
		}
	}
}

func (recorder *mixedRecorder) writeNextFrame(queues *mixedFrameQueues) {
	if len(queues.toIMS) == 0 && len(queues.fromIMS) == 0 && !queues.started {
		return
	}
	queues.started = true
	left, right := popMixedFrame(&queues.toIMS), popMixedFrame(&queues.fromIMS)
	pcm := mixPCMFrames(left, right)
	data := make([]byte, len(pcm)*2)
	for index, sample := range pcm {
		binary.LittleEndian.PutUint16(data[index*2:], uint16(sample))
	}
	written, err := recorder.file.Write(data)
	recorder.dataBytes += uint32(written)
	if err != nil || written != len(data) {
		recorder.addError(errors.Join(err, io.ErrShortWrite))
	}
}

func (recorder *mixedRecorder) drainFrames(queues *mixedFrameQueues) {
	for {
		select {
		case frame := <-recorder.toIMS:
			queues.toIMS = append(queues.toIMS, frame)
		case frame := <-recorder.fromIMS:
			queues.fromIMS = append(queues.fromIMS, frame)
		default:
			for len(queues.toIMS) > 0 || len(queues.fromIMS) > 0 {
				recorder.writeNextFrame(queues)
			}
			return
		}
	}
}

func (recorder *mixedRecorder) finalize() {
	if recorder.dataBytes == 0 {
		recorder.addError(errors.New("phone: mixed recording contains no audio frames"))
	}
	header := mixedWAVHeader(recorder.dataBytes)
	if _, err := recorder.file.Seek(0, io.SeekStart); err != nil {
		recorder.addError(fmt.Errorf("phone: seek mixed recording: %w", err))
	} else if _, err := recorder.file.Write(header); err != nil {
		recorder.addError(fmt.Errorf("phone: finalize mixed recording: %w", err))
	}
	if err := recorder.file.Close(); err != nil {
		recorder.addError(fmt.Errorf("phone: close mixed recording: %w", err))
	}
}

func (recorder *mixedRecorder) addError(err error) {
	if err == nil {
		return
	}
	recorder.resultMu.Lock()
	recorder.resultErr = errors.Join(recorder.resultErr, err)
	recorder.resultMu.Unlock()
}

func popMixedFrame(queue *[][]int16) []int16 {
	if len(*queue) == 0 {
		return nil
	}
	frame := (*queue)[0]
	*queue = (*queue)[1:]
	return frame
}

func mixPCMFrames(left, right []int16) []int16 {
	result := make([]int16, browserSamplesPerFrame)
	for index := range result {
		leftSample, rightSample := pcmSample(left, index), pcmSample(right, index)
		if left != nil && right != nil {
			result[index] = int16((int(leftSample) + int(rightSample)) / 2)
		} else {
			result[index] = leftSample + rightSample
		}
	}
	return result
}

func pcmSample(frame []int16, index int) int16 {
	if index >= len(frame) {
		return 0
	}
	return frame[index]
}

func mixedWAVHeader(dataBytes uint32) []byte {
	header := make([]byte, mixedWAVHeaderBytes)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], 36+dataBytes)
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], mixedSampleRate)
	binary.LittleEndian.PutUint32(header[28:32], mixedSampleRate*2)
	binary.LittleEndian.PutUint16(header[32:34], 2)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], dataBytes)
	return header
}
