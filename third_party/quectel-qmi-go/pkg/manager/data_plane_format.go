package manager

import (
	"context"
	"fmt"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

func desiredDataFormat(qmap bool) qmi.DataFormat {
	aggregation := qmi.DataAggregationProtocolDisabled
	if qmap {
		aggregation = qmi.DataAggregationProtocolQMAP
	}
	return qmi.DataFormat{
		LinkProtocol:      qmi.LinkProtocolIP,
		UlDataAggregation: aggregation,
		DlDataAggregation: aggregation,
	}
}

func dataFormatMatches(current *qmi.DataFormat, desired qmi.DataFormat) bool {
	return current != nil &&
		current.LinkProtocol == desired.LinkProtocol &&
		current.UlDataAggregation == desired.UlDataAggregation &&
		current.DlDataAggregation == desired.DlDataAggregation
}

func (m *Manager) getDataFormat(ctx context.Context) (*qmi.DataFormat, error) {
	if m.getDataFormatHook != nil {
		return m.getDataFormatHook(ctx)
	}
	return m.wda.GetDataFormat(ctx)
}

func (m *Manager) setDataFormat(ctx context.Context, format qmi.DataFormat) error {
	if m.setDataFormatHook != nil {
		return m.setDataFormatHook(ctx, format)
	}
	return m.wda.SetDataFormat(ctx, format)
}

func (m *Manager) ensureModemDataFormat(parent context.Context) error {
	desired := desiredDataFormat(m.cfg.MuxID > 0)
	ctx, cancel := contextWithMaxTimeout(parent, m.cfg.Timeouts.StatusCheck)
	defer cancel()

	current, err := m.getDataFormat(ctx)
	if err != nil {
		return fmt.Errorf("query current WDA data format: %w", err)
	}
	if dataFormatMatches(current, desired) {
		return nil
	}
	if err := m.setDataFormat(ctx, desired); err != nil {
		return fmt.Errorf("set WDA data format (qmap=%t): %w", m.cfg.MuxID > 0, err)
	}
	current, err = m.getDataFormat(ctx)
	if err != nil {
		return fmt.Errorf("verify WDA data format: %w", err)
	}
	if !dataFormatMatches(current, desired) {
		return fmt.Errorf("WDA data format verification mismatch: got=%+v want=%+v", current, desired)
	}
	return nil
}
