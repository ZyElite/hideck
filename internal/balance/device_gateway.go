package balance

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/carrierquery"
	"github.com/iniwex5/vohive/internal/db"
	"github.com/iniwex5/vohive/internal/device"
	"github.com/iniwex5/vohive/pkg/smscodec"
)

const backendUSSDTimeout = 45 * time.Second

type PoolGateway struct {
	pool *device.Pool
}

func NewPoolGateway(pool *device.Pool) *PoolGateway {
	return &PoolGateway{pool: pool}
}

func (g *PoolGateway) Snapshot(deviceID string) (DeviceSnapshot, error) {
	if g == nil || g.pool == nil {
		return DeviceSnapshot{}, ErrDeviceNotFound
	}
	worker := g.pool.GetWorker(deviceID)
	if worker == nil {
		return DeviceSnapshot{}, ErrDeviceNotFound
	}
	status := worker.GetCachedDeviceStatus()
	if strings.TrimSpace(status.ICCID) == "" {
		return DeviceSnapshot{}, ErrIdentityMissing
	}
	return DeviceSnapshot{DeviceID: worker.ID, ICCID: status.ICCID, MCC: status.NativeMCC,
		MNC: status.NativeMNC, SPN: status.NativeSPN, VoWiFiActive: g.pool.IsVoWiFiActive(worker.ID)}, nil
}

func (g *PoolGateway) SendVoWiFiSMS(ctx context.Context, deviceID, destination, payload string) error {
	_, err := g.pool.SendVoWiFiSMSWithOptions(ctx, deviceID, destination, payload, smscodec.SubmitOptions{})
	return err
}

func (g *PoolGateway) SendBackendSMS(_ context.Context, deviceID, destination, payload string) error {
	worker := g.pool.GetWorker(deviceID)
	if worker == nil {
		return ErrDeviceNotFound
	}
	return worker.SendSMSWithOptions(destination, payload, smscodec.SubmitOptions{})
}

func (g *PoolGateway) SendVoWiFiUSSD(ctx context.Context, deviceID, code string) (USSDResponse, error) {
	result, err := g.pool.SendVoWiFiUSSD(ctx, deviceID, code)
	if err != nil {
		return USSDResponse{}, err
	}
	return USSDResponse{Text: result.Text, Raw: firstNonEmpty(result.RawText, result.RawXML)}, nil
}

func (g *PoolGateway) SendBackendUSSD(ctx context.Context, deviceID, code string) (USSDResponse, error) {
	worker := g.pool.GetWorker(deviceID)
	if worker == nil {
		return USSDResponse{}, ErrDeviceNotFound
	}
	provider, ok := worker.Backend.(backend.USSDProvider)
	if !ok || provider == nil {
		return USSDResponse{}, fmt.Errorf("设备 %s 的当前后端不支持 USSD", deviceID)
	}
	result, err := provider.ExecuteUSSD(ctx, code, backendUSSDTimeout)
	if err != nil {
		return USSDResponse{}, err
	}
	return USSDResponse{Text: result.Text, Raw: result.RawText}, nil
}

type DBCustomRuleSource struct{}

func (DBCustomRuleSource) ListCustomCarrierQueryRules() ([]carrierquery.Rule, error) {
	return db.ListCustomCarrierQueryRules()
}
