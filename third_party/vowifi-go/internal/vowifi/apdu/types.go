package apdu

type LogicalChannelTransport interface {
	OpenLogicalChannel(aid string) (int, error)
	CloseLogicalChannel(channel int) error
	TransmitAPDU(channel int, hexAPDU string) (string, error)
}

type LogicalChannelAIDResolver interface {
	ResolveLogicalChannelAID(app, fallbackAID string) (aid, source string, err error)
}
