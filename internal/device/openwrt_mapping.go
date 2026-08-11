package device

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const dynamicInterfaceOperationTimeout = 10 * time.Second

type DynamicInterfaceMapper interface {
	Enabled() bool
	SetEnabled(bool)
	Validate() error
	Add(ctx context.Context, deviceID, dataInterface string) error
	Remove(ctx context.Context, deviceID string) error
}

type disabledDynamicInterfaceMapper struct{}

func (disabledDynamicInterfaceMapper) Enabled() bool   { return false }
func (disabledDynamicInterfaceMapper) SetEnabled(bool) {}
func (disabledDynamicInterfaceMapper) Validate() error {
	return errors.New("OpenWrt 动态接口映射器未注入")
}
func (disabledDynamicInterfaceMapper) Add(context.Context, string, string) error { return nil }
func (disabledDynamicInterfaceMapper) Remove(context.Context, string) error      { return nil }

type dataInterfaceProvider interface {
	DataInterface() string
}

func (p *Pool) OpenWRTDynamicInterfacesEnabled() bool {
	return p != nil && p.dynamicInterfaceMapper != nil && p.dynamicInterfaceMapper.Enabled()
}

func (p *Pool) ConfigureOpenWRTDynamicInterfaces(ctx context.Context, enabled bool) error {
	if p == nil || p.dynamicInterfaceMapper == nil {
		return errors.New("OpenWrt 动态接口映射器未初始化")
	}
	if enabled {
		if err := p.dynamicInterfaceMapper.Validate(); err != nil {
			return err
		}
		p.dynamicInterfaceMapper.SetEnabled(true)
		if err := p.reconcileDynamicInterfaceMappings(ctx); err != nil {
			cleanupErr := p.removeAllDynamicInterfaceMappings(ctx)
			p.dynamicInterfaceMapper.SetEnabled(false)
			return errors.Join(err, cleanupErr)
		}
		return nil
	}
	if err := p.removeAllDynamicInterfaceMappings(ctx); err != nil {
		return err
	}
	p.dynamicInterfaceMapper.SetEnabled(false)
	return nil
}

func (p *Pool) ensureDynamicInterfaceMapping(ctx context.Context, worker *Worker) error {
	if p == nil || worker == nil || p.dynamicInterfaceMapper == nil || !p.dynamicInterfaceMapper.Enabled() {
		return nil
	}
	controller := worker.NetworkController()
	provider, ok := controller.(dataInterfaceProvider)
	if !ok {
		return fmt.Errorf("设备 %s 的数据面未提供实际网卡名", worker.ID)
	}
	dataInterface := strings.TrimSpace(provider.DataInterface())
	if dataInterface == "" {
		return fmt.Errorf("设备 %s 的实际数据网卡名为空", worker.ID)
	}
	opCtx, cancel := mappingContext(ctx)
	defer cancel()
	return p.dynamicInterfaceMapper.Add(opCtx, worker.ID, dataInterface)
}

func (p *Pool) removeDynamicInterfaceMapping(ctx context.Context, deviceID string) error {
	if p == nil || p.dynamicInterfaceMapper == nil {
		return nil
	}
	opCtx, cancel := mappingContext(ctx)
	defer cancel()
	return p.dynamicInterfaceMapper.Remove(opCtx, deviceID)
}

func (p *Pool) reconcileDynamicInterfaceMappings(ctx context.Context) error {
	var result error
	for _, worker := range p.GetAllWorkers() {
		controller := worker.NetworkController()
		if controller == nil || !controller.IsConnected() {
			continue
		}
		if err := p.ensureDynamicInterfaceMapping(ctx, worker); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func (p *Pool) removeAllDynamicInterfaceMappings(ctx context.Context) error {
	var result error
	for _, worker := range p.GetAllWorkers() {
		if err := p.removeDynamicInterfaceMapping(ctx, worker.ID); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func mappingContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, dynamicInterfaceOperationTimeout)
}
