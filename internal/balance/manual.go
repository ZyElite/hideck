package balance

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const (
	manualBalanceIDPrefix = "manual:"
	maxManualAmountLength = 64
	maxCurrencyLength     = 12
)

var manualAmountPattern = regexp.MustCompile(`^-?[0-9]+(?:[.,][0-9]+)?$`)

func (s *Service) SetManualBalance(ctx context.Context, deviceID, amount, currency string) (Query, error) {
	snapshot, err := s.gateway.Snapshot(strings.TrimSpace(deviceID))
	if err != nil {
		return Query{}, err
	}
	normalizedAmount, normalizedCurrency, err := normalizeManualBalance(amount, currency)
	if err != nil {
		return Query{}, err
	}

	now := s.now()
	id := manualBalanceID(snapshot.DeviceID)
	createdAt := now
	if existing, found, getErr := s.repo.Get(ctx, id); getErr != nil {
		return Query{}, getErr
	} else if found {
		createdAt = existing.CreatedAt
	}
	query := Query{
		ID: id, DeviceID: snapshot.DeviceID, ICCID: snapshot.ICCID, RuleID: TransportManual,
		Transport: TransportManual, State: StateCompleted, ParseState: ParseManual,
		Amount: normalizedAmount, Currency: normalizedCurrency,
		Summary:   strings.TrimSpace(normalizedAmount + " " + normalizedCurrency),
		StartedAt: now, ExpiresAt: now, CompletedAt: &now, CreatedAt: createdAt, UpdatedAt: now,
	}
	if err := s.repo.SaveManual(ctx, query); err != nil {
		return Query{}, err
	}
	return query, nil
}

func (s *Service) ClearManualBalance(ctx context.Context, deviceID string) (bool, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return false, ErrDeviceNotFound
	}
	return s.repo.DeleteManual(ctx, deviceID)
}

func (s *Service) GetManualBalance(ctx context.Context, deviceID string) (Query, bool, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return Query{}, false, ErrDeviceNotFound
	}
	query, found, err := s.repo.Get(ctx, manualBalanceID(deviceID))
	if err != nil || !found {
		return Query{}, found, err
	}
	if query.DeviceID != deviceID || query.Transport != TransportManual {
		return Query{}, false, nil
	}
	return query, true, nil
}

func normalizeManualBalance(amount, currency string) (string, string, error) {
	amount = strings.TrimSpace(amount)
	if len(amount) == 0 || len(amount) > maxManualAmountLength || !manualAmountPattern.MatchString(amount) {
		return "", "", fmt.Errorf("%w: 金额必须是数字", ErrInvalidManual)
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if len(currency) > maxCurrencyLength {
		return "", "", fmt.Errorf("%w: 币种不能超过 %d 个字符", ErrInvalidManual, maxCurrencyLength)
	}
	return strings.ReplaceAll(amount, ",", "."), currency, nil
}

func manualBalanceID(deviceID string) string {
	return manualBalanceIDPrefix + strings.TrimSpace(deviceID)
}
