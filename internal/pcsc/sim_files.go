package pcsc

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

func readIdentity(ctx context.Context, card Card, pin string) (Identity, error) {
	identity := Identity{PINTries: -1}
	var err error
	identity.ICCID, err = readICCID(ctx, card)
	if err != nil {
		return identity, err
	}
	identity.USIMAID, err = selectUSIM(ctx, card)
	if err != nil {
		return identity, err
	}
	if err := verifyPIN(ctx, card, pin); err != nil {
		identity.PINRequired = errors.Is(err, ErrPINRequired) || errors.Is(err, ErrPINTriesLow)
		var pinErr *PINError
		if errors.As(err, &pinErr) {
			identity.PINTries = pinErr.Tries
		}
		return identity, err
	}
	if err := selectFile(ctx, card, []byte{0x6F, 0x07}); err != nil {
		return identity, fmt.Errorf("pcsc: select EF_IMSI: %w", err)
	}
	imsiData, err := readBinary(ctx, card, 9)
	if err != nil {
		return identity, fmt.Errorf("pcsc: read EF_IMSI: %w", err)
	}
	if identity.IMSI, err = decodeIMSI(imsiData); err != nil {
		return identity, err
	}
	if reselectUSIM(ctx, card, identity.USIMAID) {
		identity.MNCLength = readMNCLength(ctx, card)
	}
	if reselectUSIM(ctx, card, identity.USIMAID) {
		identity.SPN = readSPN(ctx, card)
	}
	if reselectUSIM(ctx, card, identity.USIMAID) {
		identity.SMSC = readSMSC(ctx, card)
	}
	return identity, nil
}

func reselectUSIM(ctx context.Context, card Card, aid []byte) bool {
	_, err := selectApplication(ctx, card, aid)
	return err == nil
}

func readMNCLength(ctx context.Context, card Card) int {
	if err := selectFile(ctx, card, []byte{0x6F, 0xAD}); err != nil {
		return 0
	}
	data, err := readBinary(ctx, card, 4)
	if err != nil || len(data) < 4 {
		return 0
	}
	length := int(data[3] & 0x0F)
	if length == 2 || length == 3 {
		return length
	}
	return 0
}

func readICCID(ctx context.Context, card Card) (string, error) {
	if err := selectMF(ctx, card); err != nil {
		return "", err
	}
	if err := selectFile(ctx, card, []byte{0x2F, 0xE2}); err != nil {
		return "", fmt.Errorf("pcsc: select EF_ICCID: %w", err)
	}
	data, err := readBinary(ctx, card, 10)
	if err != nil {
		return "", fmt.Errorf("pcsc: read EF_ICCID: %w", err)
	}
	value := decodeSwappedBCD(data, false)
	if len(value) < 18 || len(value) > 22 {
		return "", errors.New("pcsc: card returned an invalid ICCID")
	}
	return value, nil
}

func selectUSIM(ctx context.Context, card Card) ([]byte, error) {
	if err := selectMF(ctx, card); err != nil {
		return nil, err
	}
	if err := selectFile(ctx, card, []byte{0x2F, 0x00}); err != nil {
		return nil, fmt.Errorf("pcsc: select EF_DIR: %w", err)
	}
	for record := 1; record <= 32; record++ {
		data, sw, err := card.Transmit(ctx, []byte{0x00, 0xB2, byte(record), 0x04, 0x00})
		if err != nil {
			return nil, err
		}
		if sw == 0x6A83 || sw == 0x9402 {
			break
		}
		if sw != 0x9000 {
			continue
		}
		aid := findTLV(data, 0x4F)
		if strings.HasPrefix(strings.ToUpper(hex.EncodeToString(aid)), usimAIDPrefix) {
			if _, err := selectApplication(ctx, card, aid); err != nil {
				return nil, err
			}
			return aid, nil
		}
	}
	return nil, ErrUSIMUnavailable
}

func selectMF(ctx context.Context, card Card) error {
	_, sw, err := card.Transmit(ctx, []byte{0x00, 0xA4, 0x00, 0x04, 0x02, 0x3F, 0x00, 0x00})
	return requireStatus("select MF", sw, err)
}

func selectFile(ctx context.Context, card Card, fileID []byte) error {
	if len(fileID) != 2 {
		return errors.New("pcsc: invalid file identifier")
	}
	_, sw, err := card.Transmit(ctx, []byte{0x00, 0xA4, 0x00, 0x04, 0x02, fileID[0], fileID[1], 0x00})
	return requireStatus("select file", sw, err)
}

func selectApplication(ctx context.Context, card Card, aid []byte) ([]byte, error) {
	if len(aid) == 0 || len(aid) > 32 {
		return nil, errors.New("pcsc: invalid USIM AID")
	}
	apdu := []byte{0x00, 0xA4, 0x04, 0x04, byte(len(aid))}
	apdu = append(apdu, aid...)
	apdu = append(apdu, 0x00)
	data, sw, err := card.Transmit(ctx, apdu)
	if err := requireStatus("select USIM application", sw, err); err != nil {
		return nil, err
	}
	return data, nil
}

func readBinary(ctx context.Context, card Card, length int) ([]byte, error) {
	if length <= 0 || length > 256 {
		return nil, errors.New("pcsc: invalid binary read length")
	}
	le := byte(length)
	if length == 256 {
		le = 0
	}
	data, sw, err := card.Transmit(ctx, []byte{0x00, 0xB0, 0x00, 0x00, le})
	if err := requireStatus("read binary", sw, err); err != nil {
		return nil, err
	}
	return data, nil
}

func verifyPIN(ctx context.Context, card Card, pin string) error {
	pin = strings.TrimSpace(pin)
	_, sw, err := card.Transmit(ctx, []byte{0x00, 0x20, 0x00, 0x01, 0x00})
	if err != nil {
		return fmt.Errorf("pcsc: SIM PIN status check failed: %w", err)
	}
	if sw == 0x9000 {
		return nil
	}
	tries := pinTries(sw)
	if pin == "" {
		if tries >= 0 {
			return &PINError{Kind: ErrPINRequired, Tries: tries}
		}
		if sw == 0x6982 || sw == 0x9804 {
			return &PINError{Kind: ErrPINRequired, Tries: -1}
		}
		return fmt.Errorf("pcsc: SIM PIN status check failed with status %04X", sw)
	}
	if len(pin) < 4 || len(pin) > 8 || !decimalDigits(pin) {
		return errors.New("pcsc: SIM PIN must contain 4 to 8 digits")
	}
	if tries >= 0 && tries <= 2 {
		return &PINError{Kind: ErrPINTriesLow, Tries: tries}
	}
	body := bytes.Repeat([]byte{0xFF}, 8)
	copy(body, pin)
	apdu := append([]byte{0x00, 0x20, 0x00, 0x01, 0x08}, body...)
	_, sw, err = card.Transmit(ctx, apdu)
	if err != nil {
		return fmt.Errorf("pcsc: SIM PIN verification transport failed: %w", err)
	}
	if sw == 0x9000 {
		return nil
	}
	if tries = pinTries(sw); tries >= 0 {
		return &PINError{Kind: ErrPINRejected, Tries: tries}
	}
	return ErrPINRejected
}

func pinTries(sw uint16) int {
	if sw&0xFFF0 == 0x63C0 {
		return int(sw & 0x000F)
	}
	return -1
}

func requireStatus(operation string, sw uint16, err error) error {
	if err != nil {
		return fmt.Errorf("pcsc: %s transport failed: %w", operation, err)
	}
	if sw == 0x9000 {
		return nil
	}
	if sw == 0x6982 || sw == 0x9804 {
		return &PINError{Kind: ErrPINRequired, Tries: -1}
	}
	return fmt.Errorf("pcsc: %s failed with status %04X", operation, sw)
}
