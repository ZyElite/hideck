package imscore

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const (
	registrationEventPackage = "reg"
	reginfoContentType       = "application/reginfo+xml"
	reginfoSummaryLimit      = 6
	reginfoReconnectDelay    = 250 * time.Millisecond
)

type regInfoDocument struct {
	Registrations []regInfoRegistration `xml:"registration"`
}

type regInfoRegistration struct {
	AOR      string           `xml:"aor,attr"`
	Contacts []regInfoContact `xml:"contact"`
}

type regInfoContact struct {
	ID      string `xml:"id,attr"`
	State   string `xml:"state,attr"`
	Event   string `xml:"event,attr"`
	Expires string `xml:"expires,attr"`
	URI     string `xml:"uri"`
}

func (s *Service) handleRegistrationNotification(raw string) {
	event := rawSIPHeaderValue(raw, "Event")
	logging.Info("IMS NOTIFY acknowledged", "event", event)
	if !isRegistrationNotification(raw) {
		return
	}
	body, err := rawSIPBody(raw)
	if err != nil {
		logging.WarnRate("ims-reginfo-body", "IMS reginfo body is invalid", "err", err)
		return
	}
	document, err := parseReginfoXML(body)
	if err != nil {
		logging.WarnRate("ims-reginfo-xml", "IMS reginfo XML is invalid", "err", err)
		return
	}
	aor := extractReginfoAORFromDocument(document)
	if aor != "" {
		s.mu.Lock()
		s.reginfoAOR = aor
		s.mu.Unlock()
	}
	if summary := summarizeReginfoDocument(document); summary != "" {
		logging.RunDebug("IMS reginfo", "device", s.DeviceID(), "summary", summary)
	}
	if s.myContactTerminated(document) {
		logging.WarnRate("ims-reginfo-terminated-"+s.DeviceID(),
			"IMS registration binding terminated", "device", s.DeviceID(), "aor", aor)
		s.reRegisterAfterDelay(reginfoReconnectDelay)
	}
}

func isRegistrationNotification(raw string) bool {
	event, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(rawSIPHeaderValue(raw, "Event"))), ";")
	contentType := strings.ToLower(strings.TrimSpace(rawSIPHeaderValue(raw, "Content-Type")))
	return strings.TrimSpace(event) == registrationEventPackage && strings.Contains(contentType, "reginfo+xml")
}

func parseReginfoXML(body []byte) (*regInfoDocument, error) {
	if len(body) == 0 {
		return nil, errors.New("empty reginfo body")
	}
	var document regInfoDocument
	if err := xml.Unmarshal(body, &document); err != nil {
		return nil, err
	}
	return &document, nil
}

func extractReginfoAOR(body []byte) string {
	document, err := parseReginfoXML(body)
	if err != nil {
		return ""
	}
	return extractReginfoAORFromDocument(document)
}

func extractReginfoAORFromDocument(document *regInfoDocument) string {
	first := ""
	for _, registration := range document.Registrations {
		aor := strings.TrimSpace(registration.AOR)
		if aor == "" {
			continue
		}
		if first == "" {
			first = aor
		}
		identity := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(aor), "sips:"), "sip:")
		if strings.HasPrefix(identity, "+") {
			return aor
		}
	}
	return first
}

func summarizeReginfoXML(body []byte) string {
	document, err := parseReginfoXML(body)
	if err != nil {
		return ""
	}
	return summarizeReginfoDocument(document)
}

func summarizeReginfoDocument(document *regInfoDocument) string {
	if document == nil {
		return ""
	}
	parts := make([]string, 0, reginfoSummaryLimit)
	for _, registration := range document.Registrations {
		for _, contact := range registration.Contacts {
			parts = append(parts, summarizeReginfoContact(contact))
			if len(parts) == reginfoSummaryLimit {
				return formatReginfoSummary(extractReginfoAORFromDocument(document), parts)
			}
		}
	}
	return formatReginfoSummary(extractReginfoAORFromDocument(document), parts)
}

func summarizeReginfoContact(contact regInfoContact) string {
	return fmt.Sprintf("id=%s,state=%s,event=%s,expires=%s,uri=%s",
		strings.TrimSpace(contact.ID), strings.TrimSpace(contact.State),
		strings.TrimSpace(contact.Event), strings.TrimSpace(contact.Expires), strings.TrimSpace(contact.URI))
}

func formatReginfoSummary(aor string, contacts []string) string {
	if strings.TrimSpace(aor) == "" && len(contacts) == 0 {
		return ""
	}
	prefix := ""
	if strings.TrimSpace(aor) != "" {
		prefix = "aor=" + strings.TrimSpace(aor) + " "
	}
	return prefix + "contacts=" + strings.Join(contacts, "|")
}

func (s *Service) isMyContactTerminated(raw string) bool {
	if !isRegistrationNotification(raw) {
		return false
	}
	body, err := rawSIPBody(raw)
	if err != nil {
		return false
	}
	document, err := parseReginfoXML(body)
	return err == nil && s.myContactTerminated(document)
}

func (s *Service) myContactTerminated(document *regInfoDocument) bool {
	s.mu.RLock()
	contactID, contactNeedle := s.registrationContactIdentityLocked()
	s.mu.RUnlock()
	if contactID == "" && contactNeedle == "" {
		return false
	}
	active, terminated := matchingReginfoStates(document, contactID, contactNeedle)
	return terminated && !active
}

func (s *Service) registrationContactIdentityLocked() (string, string) {
	if s.regSession == nil {
		return "", ""
	}
	contactID := strings.TrimSpace(s.regSession.contactUser)
	return contactID, strings.Trim(contactID, "\"")
}

func matchingReginfoStates(document *regInfoDocument, contactID, contactNeedle string) (bool, bool) {
	if document == nil {
		return false, false
	}
	active, terminated := false, false
	for _, registration := range document.Registrations {
		for _, contact := range registration.Contacts {
			if !reginfoContactMatches(contact, contactID, contactNeedle) {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(contact.State)) {
			case "active", "registered":
				active = true
			case "terminated":
				terminated = true
			}
		}
	}
	return active, terminated
}

func reginfoContactMatches(contact regInfoContact, contactID, contactNeedle string) bool {
	if contactID != "" && strings.TrimSpace(contact.ID) == contactID {
		return true
	}
	return contactNeedle != "" && strings.Contains(strings.TrimSpace(contact.URI), contactNeedle)
}

func (s *Service) reRegisterAfterDelay(delay time.Duration) {
	if s == nil || !s.notifyReconnectPending.CompareAndSwap(false, true) {
		return
	}
	if delay < 0 {
		delay = 0
	}
	s.networkDone.Add(1)
	go s.runDelayedReRegistration(delay)
}

func (s *Service) runDelayedReRegistration(delay time.Duration) {
	defer s.networkDone.Done()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-s.stop:
		s.notifyReconnectPending.Store(false)
		return
	case <-timer.C:
		s.notifyReconnectPending.Store(false)
	}
	logging.RunDebug("IMS reginfo re-registration", "device", s.DeviceID())
	ctx, cancel := registrationContextUntilStop(s.stop)
	defer cancel()
	if err := s.Register(ctx); err != nil {
		if s.stopped() {
			logging.RunDebug("IMS reginfo re-registration canceled", "device", s.DeviceID())
			return
		}
		logging.WarnRate("ims-reginfo-reregister-"+s.DeviceID(),
			"IMS reginfo re-registration failed", "device", s.DeviceID(), "err", err)
		s.reportRegistrationRuntimeError(fmt.Errorf("imscore: reginfo re-registration failed: %w", err))
		return
	}
	logging.RunDebug("IMS reginfo re-registration succeeded", "device", s.DeviceID())
}

func registrationContextUntilStop(stop <-chan struct{}) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}
