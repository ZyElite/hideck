package device

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/yibaiba/hideck/internal/backend"
	"github.com/yibaiba/hideck/internal/pcsc"
	"github.com/yibaiba/hideck/internal/simaid"
	"github.com/iniwex5/vowifi-go/runtimehost"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
)

type pcscModemAdapter struct {
	deviceID string
	backend  backend.DeviceBackend
}

func newPCSCModemAdapter(deviceID string, deviceBackend backend.DeviceBackend) *pcscModemAdapter {
	return &pcscModemAdapter{deviceID: deviceID, backend: deviceBackend}
}

func (adapter *pcscModemAdapter) DeviceID() string { return adapter.deviceID }

func (adapter *pcscModemAdapter) IsHealthy() bool {
	present, err := adapter.QuerySIMInserted()
	return err == nil && present
}

func (adapter *pcscModemAdapter) IsSimInserted() bool { return adapter.IsHealthy() }

func (adapter *pcscModemAdapter) QuerySIMInserted() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return adapter.backend.IsSimInserted(ctx)
}

func (*pcscModemAdapter) GetRegStatus() (int, string) { return 0, "not_applicable" }

func (*pcscModemAdapter) GetNetworkMode() string { return "wifi" }

func (*pcscModemAdapter) ExecuteATSilent(command string, _ time.Duration) (string, error) {
	return "", fmt.Errorf("PC/SC 读卡器不支持 AT 指令: %s", command)
}

func (adapter *pcscModemAdapter) OpenLogicalChannel(aid string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return adapter.backend.OpenLogicalChannel(ctx, aid)
}

func (adapter *pcscModemAdapter) ResolveLogicalChannelAID(app, fallback string) (string, string, error) {
	resolver, ok := adapter.backend.(backend.SIMAuthAIDResolver)
	if !ok {
		return fallback, "fallback", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return resolver.ResolveSIMAuthAID(ctx, app, fallback)
}

func (adapter *pcscModemAdapter) CloseLogicalChannel(logical int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return adapter.backend.CloseLogicalChannel(ctx, logical)
}

func (adapter *pcscModemAdapter) TransmitAPDU(logical int, command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return adapter.backend.TransmitAPDU(ctx, logical, command)
}

func (adapter *pcscModemAdapter) GetISIMIdentity() (identity.Identity, error) {
	result, err := identity.ReadISIMIdentityFromLogicalChannel(adapter)
	if errors.Is(err, pcsc.ErrApplicationNotFound) || errors.Is(err, simaid.ErrApplicationNotFound) || errors.Is(err, backend.ErrSIMAuthApplicationUnavailable) {
		return identity.Identity{}, fmt.Errorf("%w: %v", identity.ErrISIMUnavailable, err)
	}
	return result, err
}

func (*pcscModemAdapter) Stop() {}

func (*pcscModemAdapter) Capabilities() runtimehost.ModemCapabilities {
	return runtimehost.ModemCapabilities{
		SIM: true, ISIMIdentity: true, Reader: true, HasISIM: true, HasUSIM: true,
	}
}

func (adapter *pcscModemAdapter) IMSIdentityProvider() runtimehost.IMSIdentityProvider {
	return adapter
}
