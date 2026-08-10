package voice

import (
	"errors"
	"net"
	"strconv"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

func (a *Agent) imsEndpoint() imsendpoint.Endpoint {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.endpoint
}

func (a *Agent) imsSnapshot() imsendpoint.Snapshot {
	if endpoint := a.imsEndpoint(); endpoint != nil {
		return endpoint.Snapshot()
	}
	return imsendpoint.Snapshot{}
}

func (a *Agent) registeredDialogProfile() (imscore.SIPDialogProfile, error) {
	endpoint := a.imsEndpoint()
	if endpoint == nil {
		return imscore.SIPDialogProfile{}, errors.New("voice: IMS endpoint is unavailable")
	}
	if provider, ok := endpoint.(interface {
		RegisteredSIPDialogProfile() (imscore.SIPDialogProfile, error)
	}); ok {
		return provider.RegisteredSIPDialogProfile()
	}
	if !endpoint.IsRegistered() {
		return imscore.SIPDialogProfile{}, errors.New("voice: IMS is not registered")
	}
	snapshot := endpoint.Snapshot()
	if strings.TrimSpace(snapshot.IMPU) == "" {
		return imscore.SIPDialogProfile{}, errors.New("voice: registered public identity is unavailable")
	}
	contact := endpointContactURI(snapshot)
	return imscore.SIPDialogProfile{
		LocalURI: snapshot.IMPU, ContactURI: contact, ContactHeader: "<" + contact + ">",
		Domain: snapshot.Realm, LocalAddress: snapshot.LocalAddr,
		Transport: snapshot.Transport, ServiceRoute: snapshot.ServiceRoute,
		SecurityVerify: snapshot.SecVerify, PANI: snapshot.PAccessNetworkInfo,
		UserAgent: snapshot.UserAgent, InitialCSeq: int(endpoint.NextCSeq()),
	}, nil
}

func endpointContactURI(snapshot imsendpoint.Snapshot) string {
	if value := strings.Trim(strings.TrimSpace(snapshot.PubGRUU), "<>"); value != "" {
		return value
	}
	user := strings.TrimSpace(snapshot.ContactID)
	if user == "" {
		user = strings.TrimPrefix(strings.TrimSpace(snapshot.IMPU), "sip:")
		if at := strings.IndexByte(user, '@'); at >= 0 {
			user = user[:at]
		}
	}
	host := strings.TrimSpace(snapshot.LocalAddr)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	if snapshot.LocalPortS > 0 {
		host = net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(snapshot.LocalPortS))
	}
	return "sip:" + user + "@" + host
}

func (a *Agent) replaceIMSProvider(endpoint imsendpoint.Endpoint) error {
	if a == nil {
		return errors.New("voice: nil agent")
	}
	if endpoint == nil {
		return errors.New("voice: IMS endpoint is unavailable")
	}
	if deviceID := strings.TrimSpace(endpoint.DeviceID()); deviceID != "" && deviceID != a.deviceID {
		return errors.New("voice: IMS endpoint device does not match agent")
	}
	ims, _ := endpoint.(*imscore.Service)
	a.mu.Lock()
	started := a.started
	previousIMS := a.ims
	previousUnsubscribe := a.imsUnsubscribe
	a.imsUnsubscribe = nil
	a.endpoint = endpoint
	a.ims = ims
	if ims != nil {
		a.bus = ims.EventBus()
	}
	a.mu.Unlock()
	if previousUnsubscribe != nil {
		previousUnsubscribe()
	}
	if started && previousIMS != nil {
		previousIMS.SetVoiceRequestHandler(nil)
	}
	if a.dialog != nil {
		a.dialog.SetEndpoint(endpoint)
	}
	if started {
		a.startReplacementProvider(endpoint, ims)
	}
	return nil
}

func (a *Agent) startReplacementProvider(endpoint imsendpoint.Endpoint, ims *imscore.Service) {
	a.mu.Lock()
	active := a.started && a.endpoint == endpoint
	if active && ims != nil {
		ims.SetVoiceRequestHandler(a)
	}
	a.mu.Unlock()
	if !active {
		return
	}
	a.installIMSUnsubscribe(a.subscribeIMSEvents())
}
