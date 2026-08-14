package device

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/yibaiba/hideck/internal/backend"
	"github.com/yibaiba/hideck/internal/pcsc"
	"github.com/yibaiba/hideck/internal/simaid"
)

var errPCSCOperationUnsupported = errors.New("PC/SC 读卡器不支持蜂窝网络操作")

type pcscDeviceBackend struct {
	service  *pcsc.Service
	selector pcsc.Selector
	pinEnv   string

	identityMu sync.RWMutex
	identity   pcsc.Identity
	channelsMu sync.Mutex
	channels   map[int]*pcsc.Channel
}

func newPCSCDeviceBackend(service *pcsc.Service, selector pcsc.Selector, pinEnv string) (*pcscDeviceBackend, error) {
	if service == nil {
		return nil, pcsc.ErrUnavailable
	}
	if err := selector.Validate(); err != nil {
		return nil, err
	}
	return &pcscDeviceBackend{
		service: service, selector: selector, pinEnv: strings.TrimSpace(pinEnv),
		channels: make(map[int]*pcsc.Channel),
	}, nil
}

func (device *pcscDeviceBackend) Mode() string { return backend.BackendPCSC }

func (device *pcscDeviceBackend) Close() error {
	device.channelsMu.Lock()
	channels := device.channels
	device.channels = make(map[int]*pcsc.Channel)
	device.channelsMu.Unlock()
	var failures []error
	for logical, channel := range channels {
		failures = append(failures, channel.CloseLogicalChannel(byte(logical)), channel.Disconnect())
	}
	return errors.Join(failures...)
}

func (device *pcscDeviceBackend) simPIN() (string, error) {
	if device.pinEnv == "" {
		return "", nil
	}
	value, ok := os.LookupEnv(device.pinEnv)
	if !ok {
		return "", fmt.Errorf("pcsc: SIM PIN environment variable %s is not set", device.pinEnv)
	}
	return strings.TrimSpace(value), nil
}

func (device *pcscDeviceBackend) readIdentity(ctx context.Context) (pcsc.Identity, error) {
	pin, err := device.simPIN()
	if err != nil {
		return pcsc.Identity{}, err
	}
	identity, err := device.service.ReadIdentity(ctx, device.selector, pin)
	if err != nil {
		return identity, err
	}
	device.identityMu.Lock()
	device.identity = identity
	device.identityMu.Unlock()
	return identity, nil
}

func (device *pcscDeviceBackend) cachedIdentity() pcsc.Identity {
	device.identityMu.RLock()
	defer device.identityMu.RUnlock()
	identity := device.identity
	identity.USIMAID = append([]byte(nil), identity.USIMAID...)
	return identity
}

func (device *pcscDeviceBackend) ReadSIMIdentityLive(ctx context.Context) (liveSIMIdentitySnapshot, error) {
	identity, err := device.readIdentity(ctx)
	if err != nil {
		return liveSIMIdentitySnapshot{}, err
	}
	return liveSIMIdentitySnapshot{
		ICCID: identity.ICCID, IMSI: identity.IMSI, NativeSPN: identity.SPN,
		Metadata: simMetadataFromPCSCIdentity(identity),
	}, nil
}

func simMetadataFromPCSCIdentity(identity pcsc.Identity) *backend.SIMMetadata {
	if len(identity.IMSI) < 5 {
		return nil
	}
	mncLength := identity.MNCLength
	if mncLength != 2 && mncLength != 3 {
		mncLength = assignedPCSCMNCLength(identity.IMSI)
	}
	if mncLength == 0 {
		return nil
	}
	end := 3 + mncLength
	if len(identity.IMSI) < end {
		return nil
	}
	return &backend.SIMMetadata{NativeMCC: identity.IMSI[:3], NativeMNC: identity.IMSI[3:end]}
}

func assignedPCSCMNCLength(imsi string) int {
	for _, prefix := range []string{"20404", "23415", "23487"} {
		if strings.HasPrefix(imsi, prefix) {
			return 2
		}
	}
	return 0
}

func (device *pcscDeviceBackend) GetIMEI(context.Context) (string, error) {
	return "", nil
}

func (device *pcscDeviceBackend) GetIMSI(ctx context.Context) (string, error) {
	identity, err := device.readIdentity(ctx)
	return identity.IMSI, err
}

func (device *pcscDeviceBackend) GetICCID(ctx context.Context) (string, error) {
	identity, err := device.readIdentity(ctx)
	return identity.ICCID, err
}

func (device *pcscDeviceBackend) GetICCIDLive(ctx context.Context) (string, error) {
	return device.GetICCID(ctx)
}

func (device *pcscDeviceBackend) GetIMSILive(ctx context.Context) (string, error) {
	return device.GetIMSI(ctx)
}

func (device *pcscDeviceBackend) GetNativeSPNLive(ctx context.Context) (string, error) {
	identity, err := device.readIdentity(ctx)
	return identity.SPN, err
}

func (device *pcscDeviceBackend) GetMSISDN(context.Context) (string, error) {
	return "", errPCSCOperationUnsupported
}

func (*pcscDeviceBackend) GetRevision(context.Context) (string, error) { return "PC/SC", nil }

func (*pcscDeviceBackend) GetSignalInfo(context.Context) (*backend.SignalInfo, error) {
	return nil, errPCSCOperationUnsupported
}

func (*pcscDeviceBackend) GetServingSystem(context.Context) (*backend.ServingSystem, error) {
	return nil, errPCSCOperationUnsupported
}

func (device *pcscDeviceBackend) IsSimInserted(ctx context.Context) (bool, error) {
	readers, err := device.service.Readers(ctx)
	if err != nil {
		return false, err
	}
	reader, ok := pcsc.MatchReader(readers, device.selector)
	if !ok {
		return false, pcsc.ErrReaderNotFound
	}
	return reader.CardPresent, nil
}

func (device *pcscDeviceBackend) GetNativeMCCMNC(ctx context.Context) (string, string, error) {
	identity, err := device.readIdentity(ctx)
	if err != nil {
		return "", "", err
	}
	metadata := simMetadataFromPCSCIdentity(identity)
	if metadata == nil {
		return "", "", errors.New("pcsc: EF_AD does not provide a valid MNC length and the IMSI has no known exact HPLMN assignment")
	}
	return metadata.NativeMCC, metadata.NativeMNC, nil
}

func (device *pcscDeviceBackend) GetNativeSPN(ctx context.Context) (string, error) {
	return device.GetNativeSPNLive(ctx)
}

func (device *pcscDeviceBackend) GetSIMMetadata(ctx context.Context) (*backend.SIMMetadata, error) {
	mcc, mnc, err := device.GetNativeMCCMNC(ctx)
	if err != nil {
		return nil, err
	}
	return &backend.SIMMetadata{NativeMCC: mcc, NativeMNC: mnc}, nil
}

func (device *pcscDeviceBackend) GetSIMMetadataLive(ctx context.Context) (*backend.SIMMetadata, error) {
	return device.GetSIMMetadata(ctx)
}

func (device *pcscDeviceBackend) GetSMSC(ctx context.Context) (string, error) {
	identity, err := device.readIdentity(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(identity.SMSC) == "" {
		return "", errors.New("pcsc: SIM does not expose a service-centre address")
	}
	return identity.SMSC, nil
}

func (device *pcscDeviceBackend) OpenLogicalChannel(ctx context.Context, aidHex string) (int, error) {
	aid, err := hex.DecodeString(strings.TrimSpace(aidHex))
	if err != nil || len(aid) == 0 {
		return 0, fmt.Errorf("pcsc: invalid logical channel AID %q", aidHex)
	}
	channel := pcsc.NewChannel(device.service, device.selector, false)
	channel.SetContext(ctx)
	if err := channel.Connect(); err != nil {
		return 0, err
	}
	logical, err := channel.OpenLogicalChannel(aid)
	if err != nil {
		_ = channel.Disconnect()
		return 0, err
	}
	device.channelsMu.Lock()
	device.channels[int(logical)] = channel
	device.channelsMu.Unlock()
	return int(logical), nil
}

func (device *pcscDeviceBackend) ResolveSIMAuthAID(_ context.Context, app, fallback string) (string, string, error) {
	app = strings.ToLower(strings.TrimSpace(app))
	if app == "usim" {
		if aid := device.cachedIdentity().USIMAID; len(aid) > 0 {
			return strings.ToUpper(hex.EncodeToString(aid)), "pcsc_ef_dir", nil
		}
		return fallback, "fallback", nil
	}
	if app == "isim" {
		return fallback, "fallback", nil
	}
	return "", "", simaid.ErrApplicationNotFound
}

func (device *pcscDeviceBackend) logicalChannel(logical int, remove bool) *pcsc.Channel {
	device.channelsMu.Lock()
	defer device.channelsMu.Unlock()
	channel := device.channels[logical]
	if remove {
		delete(device.channels, logical)
	}
	return channel
}

func (device *pcscDeviceBackend) CloseLogicalChannel(ctx context.Context, logical int) error {
	channel := device.logicalChannel(logical, true)
	if channel == nil {
		return fmt.Errorf("pcsc: logical channel %d is not open", logical)
	}
	channel.SetContext(ctx)
	err := channel.CloseLogicalChannel(byte(logical))
	return errors.Join(err, channel.Disconnect())
}

func (device *pcscDeviceBackend) TransmitAPDU(ctx context.Context, logical int, command string) (string, error) {
	channel := device.logicalChannel(logical, false)
	if channel == nil {
		return "", fmt.Errorf("pcsc: logical channel %d is not open", logical)
	}
	apdu, err := hex.DecodeString(strings.TrimSpace(command))
	if err != nil {
		return "", fmt.Errorf("pcsc: invalid APDU: %w", err)
	}
	channel.SetContext(ctx)
	response, err := channel.Transmit(apdu)
	return strings.ToUpper(hex.EncodeToString(response)), err
}
