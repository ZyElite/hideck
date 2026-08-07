package identity

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const isimAIDPrefix = "A0000000871004"

// LogicalChannelAccess is the modem surface required to read ISIM identity
// files through an already selected logical channel.
type LogicalChannelAccess interface {
	OpenLogicalChannel(aid string) (int, error)
	CloseLogicalChannel(channel int) error
	TransmitAPDU(channel int, hexAPDU string) (string, error)
}

type logicalChannelAIDResolver interface {
	ResolveLogicalChannelAID(app string, fallbackAID string) (aid string, source string, err error)
}

// ReadISIMIdentityFromLogicalChannel resolves the full ISIM AID, opens a
// logical channel, reads EF_IMPI/EF_DOMAIN/EF_IMPU, and closes the channel.
func ReadISIMIdentityFromLogicalChannel(access LogicalChannelAccess) (result Identity, err error) {
	if access == nil {
		return Identity{}, errors.New("identity: no logical channel access")
	}

	aid, source, err := resolveISIMAID(access)
	if err != nil {
		return Identity{}, err
	}
	channel, err := access.OpenLogicalChannel(aid)
	if err != nil {
		return Identity{}, fmt.Errorf("identity: open ISIM logical channel (%s): %w", source, err)
	}
	defer func() {
		if closeErr := access.CloseLogicalChannel(channel); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("identity: close ISIM logical channel: %w", closeErr))
		}
	}()

	isim, err := readISIMIdentityFiles(access, channel)
	if err != nil {
		return Identity{}, fmt.Errorf("identity: read ISIM files: %w", err)
	}
	return isim, nil
}

func resolveISIMAID(access LogicalChannelAccess) (string, string, error) {
	resolver, ok := access.(logicalChannelAIDResolver)
	if !ok {
		return isimAIDPrefix, "fallback", nil
	}
	aid, source, err := resolver.ResolveLogicalChannelAID("isim", isimAIDPrefix)
	if err != nil {
		return "", source, fmt.Errorf("identity: resolve ISIM AID: %w", err)
	}
	aid = strings.ToUpper(strings.TrimSpace(aid))
	if !strings.HasPrefix(aid, isimAIDPrefix) || len(aid) <= len(isimAIDPrefix) {
		return "", source, fmt.Errorf("identity: invalid full ISIM AID: %s", aid)
	}
	if _, err := hex.DecodeString(aid); err != nil {
		return "", source, fmt.Errorf("identity: decode ISIM AID: %w", err)
	}
	if strings.TrimSpace(source) == "" {
		source = "resolver"
	}
	return aid, source, nil
}

func trimIdentityValues(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}
