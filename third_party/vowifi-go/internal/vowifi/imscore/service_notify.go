package imscore

import (
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const smsNotificationTimeLayout = "2006-01-02 15:04:05"

func (s *Service) maybeNotifySMSReady(reason string) {
	if s == nil || s.cfg == nil {
		return
	}
	s.mu.Lock()
	ready := !s.smsReadyNotified && s.smsReceiverReady &&
		strings.TrimSpace(s.cfg.SMSC) != "" &&
		s.regStatus.Load() == registrationRegistered
	if !ready {
		s.mu.Unlock()
		return
	}
	s.smsReadyNotified = true
	callback := s.onSMSReady
	deviceID := s.cfg.DeviceID
	s.mu.Unlock()
	logging.Info("IMS SMS receiver ready", "device", deviceID, "reason", strings.TrimSpace(reason))
	if callback != nil {
		callback()
	}
}

func formatVoWiFiSMSSentMessage(device, number, content string, at time.Time, parts int) string {
	return fmt.Sprintf("发送短信 / 完成\n设备    %s\n号码    %s\n通道    VoWiFi\n时间    %s\n内容    %s\n分片    %d",
		device, number, at.Format(smsNotificationTimeLayout), content, parts)
}

func (s *Service) getRuntimeEventBus() *EventBus {
	if s == nil {
		return nil
	}
	return s.bus
}

func (s *Service) publishRuntimeEvent(event events.Event) {
	if event == nil {
		return
	}
	if notification, ok := event.(events.EventLogNotify); ok && strings.TrimSpace(notification.Message) == "" {
		return
	}
	if bus := s.getRuntimeEventBus(); bus != nil {
		bus.Publish(event)
	}
}

func (s *Service) publishLogNotification(message string) {
	if s == nil || s.cfg == nil || strings.TrimSpace(message) == "" {
		return
	}
	s.publishRuntimeEvent(events.EventLogNotify{DevID: s.cfg.DeviceID, Message: message})
}
