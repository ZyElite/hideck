package identity

import internalprofile "github.com/iniwex5/vowifi-go/internal/vowifi/profile"

// LogicalChannelTransport is the recovered SIM logical-channel surface.
type LogicalChannelTransport interface {
	OpenLogicalChannel(string) (int, error)
	CloseLogicalChannel(int) error
	TransmitAPDU(int, string) (string, error)
}

type LogicalChannelAccess = LogicalChannelTransport

// ReadISIMIdentity reads EF_IMPI, EF_DOMAIN and EF_IMPU from the ISIM app.
func ReadISIMIdentity(access LogicalChannelTransport) (Identity, error) {
	isim, err := internalprofile.ReadISIMIdentity(access)
	if err != nil {
		return Identity{}, err
	}
	return Identity{IMPI: isim.IMPI, IMPU: append([]string(nil), isim.IMPU...), Domain: isim.Domain}, nil
}

// ReadISIMIdentityFromLogicalChannel resolves the full ISIM AID, opens a
// logical channel, reads EF_IMPI/EF_DOMAIN/EF_IMPU, and closes the channel.
func ReadISIMIdentityFromLogicalChannel(access LogicalChannelAccess) (Identity, error) {
	return ReadISIMIdentity(access)
}
