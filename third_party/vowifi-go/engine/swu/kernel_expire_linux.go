//go:build linux

package swu

import (
	"context"
	"errors"

	"github.com/iniwex5/netlink"
	"github.com/iniwex5/netlink/nl"
	"github.com/iniwex5/vowifi-go/engine/logger"
	"go.uber.org/zap"
)

type xfrmMonitorOpen func(<-chan struct{}) (<-chan netlink.XfrmMsg, <-chan error, error)

func openXFRMExpireMonitor(done <-chan struct{}) (<-chan netlink.XfrmMsg, <-chan error, error) {
	events := make(chan netlink.XfrmMsg)
	errors := make(chan error, 1)
	if err := netlink.XfrmMonitor(events, done, errors, nl.XFRM_MSG_EXPIRE); err != nil {
		return nil, nil, err
	}
	return events, errors, nil
}

func (x *xfrmDataPlane) StartExpireMonitor(
	parent context.Context,
	handle func(xfrmExpireEvent),
) error {
	if x == nil || handle == nil {
		return errors.New("swu: XFRM expire monitor requires a data plane and handler")
	}
	if parent == nil {
		parent = context.Background()
	}
	x.monitorMu.Lock()
	defer x.monitorMu.Unlock()
	if x.monitorClosed {
		return errors.New("swu: XFRM expire monitor is closed")
	}
	if x.monitorCancel != nil {
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	if err := ctx.Err(); err != nil {
		cancel()
		return err
	}
	open := x.monitorOpen
	if open == nil {
		open = openXFRMExpireMonitor
	}
	events, monitorErrors, err := open(ctx.Done())
	if err != nil {
		cancel()
		return err
	}
	x.monitorCancel = cancel
	x.monitorWG.Add(1)
	go x.runExpireMonitor(ctx, events, monitorErrors, handle)
	return nil
}

func (x *xfrmDataPlane) runExpireMonitor(
	ctx context.Context,
	events <-chan netlink.XfrmMsg,
	monitorErrors <-chan error,
	handle func(xfrmExpireEvent),
) {
	defer x.monitorWG.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-monitorErrors:
			if ok && err != nil && ctx.Err() == nil {
				logger.Warn("XFRM expire monitor failed", zap.Error(err))
			}
			return
		case message, ok := <-events:
			if !ok {
				return
			}
			expire, ok := message.(*netlink.XfrmMsgExpire)
			if !ok || expire.XfrmState == nil {
				continue
			}
			handle(xfrmExpireEvent{spi: uint32(expire.XfrmState.Spi), hard: expire.Hard})
		}
	}
}

func (x *xfrmDataPlane) stopExpireMonitor() {
	x.monitorMu.Lock()
	x.monitorClosed = true
	cancel := x.monitorCancel
	x.monitorMu.Unlock()
	if cancel != nil {
		cancel()
	}
	x.monitorWG.Wait()
}
