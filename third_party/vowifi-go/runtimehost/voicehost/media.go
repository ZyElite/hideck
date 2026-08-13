package voicehost

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func (g *Gateway) simulatedCaptureBasePath(deviceID, explicit string) string {
	if value := strings.TrimSpace(explicit); value != "" {
		return value
	}
	if g == nil {
		return ""
	}
	g.mu.RLock()
	directory := g.pcapDirectory
	g.mu.RUnlock()
	if directory == "" {
		return ""
	}
	deviceID = sanitizeCapturePart(deviceID)
	if deviceID == "" {
		deviceID = "device"
	}
	name := fmt.Sprintf("call_%s_%s", deviceID, time.Now().Format("20060102_150405.000000000"))
	return filepath.Join(directory, name)
}

func sanitizeCapturePart(value string) string {
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

// SetPCAPDirectory injects the output directory used by StartPCAPCurrent.
func (g *Gateway) SetPCAPDirectory(directory string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.pcapDirectory = strings.TrimSpace(directory)
	g.mu.Unlock()
}

// SetAudioTranscoder injects the host codec boundary used for MP3 publication.
func (g *Gateway) SetAudioTranscoder(transcoder AudioTranscoder) {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.audioTranscoder = transcoder
	g.mu.Unlock()
}

// StartPCAP restores the original device and output arguments.
func (g *Gateway) StartPCAP(deviceID, output string) error {
	if g == nil {
		return nil
	}
	if agent := g.internalAgent(deviceID); agent != nil {
		return g.inner.StartPCAP(deviceID, output)
	}
	agent := g.currentVoiceAgent(deviceID)
	if agent == nil {
		if g.inner == nil {
			return nil
		}
		return g.inner.StartPCAP(deviceID, output)
	}
	capture, ok := agent.(interface{ StartPCAP(string) error })
	if !ok {
		return errors.New("voicehost: agent does not support packet capture")
	}
	return capture.StartPCAP(output)
}

// StartPCAPCurrent retains injected-directory selection.
func (g *Gateway) StartPCAPCurrent(deviceID string, output ...string) error {
	if g == nil {
		return errors.New("voicehost: nil gateway")
	}
	g.mu.RLock()
	directory := g.pcapDirectory
	g.mu.RUnlock()
	if len(output) != 0 {
		directory = strings.TrimSpace(output[0])
	}
	if directory == "" {
		return errors.New("voicehost: packet capture output is not configured")
	}
	return g.StartPCAP(deviceID, directory)
}

func (g *Gateway) StopPCAP(deviceID string) error {
	if g == nil {
		return nil
	}
	if agent := g.internalAgent(deviceID); agent != nil {
		return g.inner.StopPCAP(deviceID)
	}
	agent := g.currentVoiceAgent(deviceID)
	if agent == nil {
		if g.inner == nil {
			return nil
		}
		return g.inner.StopPCAP(deviceID)
	}
	capture, ok := agent.(interface{ StopPCAP() error })
	if !ok {
		return errors.New("voicehost: agent does not support packet capture")
	}
	return capture.StopPCAP()
}

// SendDTMF is additive and resolves the real call through the device registry.
func (g *Gateway) SendDTMF(deviceID, callID, digit string) error {
	if g == nil {
		return errors.New("voicehost: nil gateway")
	}
	if agent := g.internalAgent(deviceID); agent != nil {
		return agent.SendDTMF(callID, digit)
	}
	agent := g.currentVoiceAgent(deviceID)
	if agent == nil {
		return errors.New("voicehost: no agent for device " + deviceID)
	}
	sender, ok := agent.(interface{ SendDTMF(string, string) error })
	if !ok {
		return errors.New("voicehost: agent does not support DTMF")
	}
	return sender.SendDTMF(callID, digit)
}

func (g *Gateway) currentVoiceAgent(deviceID string) voiceAgent {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.agents[strings.TrimSpace(deviceID)]
}
