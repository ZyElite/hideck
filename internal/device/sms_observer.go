package device

import (
	"strings"
	"time"

	"github.com/iniwex5/vohive/pkg/logger"
)

type InboundSMS struct {
	DeviceID string
	ICCID    string
	Sender   string
	Content  string
	Time     time.Time
}

type InboundSMSHandler func(InboundSMS) error

func (p *Pool) OnInboundSMS(handler InboundSMSHandler) {
	if p == nil || handler == nil {
		return
	}
	p.inboundSMSHandlersMu.Lock()
	p.inboundSMSHandlers = append(p.inboundSMSHandlers, handler)
	p.inboundSMSHandlersMu.Unlock()
}

func (p *Pool) notifyInboundSMS(message InboundSMS) {
	if p == nil || strings.TrimSpace(message.ICCID) == "" {
		return
	}
	p.inboundSMSHandlersMu.RLock()
	handlers := append([]InboundSMSHandler(nil), p.inboundSMSHandlers...)
	p.inboundSMSHandlersMu.RUnlock()
	for _, handler := range handlers {
		if err := handler(message); err != nil {
			logger.Warn("入站短信观察器处理失败", "device", message.DeviceID, "sender", message.Sender, "err", err)
		}
	}
}
