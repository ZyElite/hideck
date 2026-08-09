package identity

import internalprofile "github.com/iniwex5/vowifi-go/internal/vowifi/profile"

type LogicalChannelAccess = internalprofile.LogicalChannelTransport

// ReadISIMIdentityFromLogicalChannel resolves the full ISIM AID, opens a
// logical channel, reads EF_IMPI/EF_DOMAIN/EF_IMPU, and closes the channel.
func ReadISIMIdentityFromLogicalChannel(access LogicalChannelAccess) (Identity, error) {
	isim, err := internalprofile.ReadISIMIdentity(access)
	if err != nil {
		return Identity{}, err
	}
	return Identity{IMPI: isim.IMPI, IMPU: append([]string(nil), isim.IMPU...), Domain: isim.Domain}, nil
}
