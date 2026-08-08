package swu

import (
	"errors"
	"net"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func (s *Session) applyMOBIKENegotiation(payloads []ikev2.Payload) {
	for _, payload := range payloads {
		notify, ok := payload.(*ikev2.EncryptedPayloadNotify)
		if !ok || notify.NotifyType != ikev2.MOBIKE_SUPPORTED {
			continue
		}
		s.mu.Lock()
		s.mobikeSupported = true
		s.mu.Unlock()
		return
	}
}

func (s *Session) peerMOBIKEResponse(payloads []ikev2.Payload, retiredIKE bool) ([]ikev2.Payload, error) {
	if !containsMOBIKEUpdate(payloads) {
		return nil, nil
	}
	if retiredIKE {
		return nil, errors.New("swu: retired IKE SA cannot update MOBIKE addresses")
	}
	s.mu.RLock()
	supported := s.mobikeSupported
	s.mu.RUnlock()
	if !supported {
		return nil, nil
	}
	transport := s.transport()
	if transport == nil {
		return nil, errors.New("swu: peer MOBIKE update has no active transport")
	}
	localIP, remoteIP := transport.LocalIP(), transport.RemoteIP()
	if err := s.updateXFRMState(ipString(localIP), ipString(remoteIP)); err != nil {
		return nil, err
	}
	return echoedCookie2Payloads(payloads), nil
}

func containsMOBIKEUpdate(payloads []ikev2.Payload) bool {
	for _, payload := range payloads {
		notify, ok := payload.(*ikev2.EncryptedPayloadNotify)
		if ok && notify.NotifyType == ikev2.UPDATE_SA_ADDRESSES {
			return true
		}
	}
	return false
}

func echoedCookie2Payloads(payloads []ikev2.Payload) []ikev2.Payload {
	var result []ikev2.Payload
	for _, payload := range payloads {
		notify, ok := payload.(*ikev2.EncryptedPayloadNotify)
		if !ok || notify.NotifyType != ikev2.COOKIE2 || len(notify.NotifyData) == 0 {
			continue
		}
		result = append(result, &ikev2.EncryptedPayloadNotify{
			NotifyType: ikev2.COOKIE2, NotifyData: append([]byte(nil), notify.NotifyData...),
		})
	}
	return result
}

func ipString(ip net.IP) string {
	if ip == nil {
		return ""
	}
	return ip.String()
}
