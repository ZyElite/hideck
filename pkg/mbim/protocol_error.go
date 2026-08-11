package mbim

import "fmt"

// ProtocolErrorCode is an MBIM protocol-layer error from section 9.3.4.
type ProtocolErrorCode uint32

const (
	ProtocolErrorTimeoutFragment  ProtocolErrorCode = 1
	ProtocolErrorFragmentSequence ProtocolErrorCode = 2
	ProtocolErrorLengthMismatch   ProtocolErrorCode = 3
	ProtocolErrorDuplicatedTID    ProtocolErrorCode = 4
	ProtocolErrorNotOpened        ProtocolErrorCode = 5
	ProtocolErrorUnknown          ProtocolErrorCode = 6
	ProtocolErrorCancel           ProtocolErrorCode = 7
	ProtocolErrorMaxTransfer      ProtocolErrorCode = 8
)

// ProtocolError reports a FUNCTION_ERROR or a locally detected wire violation.
type ProtocolError struct {
	Code          ProtocolErrorCode
	TransactionID uint32
	Detail        string
}

func (e *ProtocolError) Error() string {
	if e == nil {
		return "mbim: protocol error"
	}
	base := fmt.Sprintf("mbim: protocol error %s(%d) tx=%d", protocolErrorName(e.Code), e.Code, e.TransactionID)
	if e.Detail == "" {
		return base
	}
	return base + ": " + e.Detail
}

func protocolErrorName(code ProtocolErrorCode) string {
	switch code {
	case ProtocolErrorTimeoutFragment:
		return "TIMEOUT_FRAGMENT"
	case ProtocolErrorFragmentSequence:
		return "FRAGMENT_OUT_OF_SEQUENCE"
	case ProtocolErrorLengthMismatch:
		return "LENGTH_MISMATCH"
	case ProtocolErrorDuplicatedTID:
		return "DUPLICATED_TID"
	case ProtocolErrorNotOpened:
		return "NOT_OPENED"
	case ProtocolErrorUnknown:
		return "UNKNOWN"
	case ProtocolErrorCancel:
		return "CANCEL"
	case ProtocolErrorMaxTransfer:
		return "MAX_TRANSFER"
	default:
		return "UNRECOGNIZED"
	}
}

func encodeProtocolError(messageType MessageType, tx uint32, code ProtocolErrorCode) []byte {
	b := make([]byte, headerLen+4)
	putHeader(b, messageType, uint32(len(b)), tx)
	le.PutUint32(b[headerLen:], uint32(code))
	return b
}

func decodeFunctionError(header Header, message []byte) error {
	if len(message) != headerLen+4 {
		return &ProtocolError{
			Code:          ProtocolErrorLengthMismatch,
			TransactionID: header.TransactionID,
			Detail:        fmt.Sprintf("FUNCTION_ERROR length=%d, want 16", len(message)),
		}
	}
	return &ProtocolError{
		Code:          ProtocolErrorCode(le.Uint32(message[headerLen:])),
		TransactionID: header.TransactionID,
	}
}
