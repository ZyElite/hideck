package eap

import "fmt"

// EAP notification codes (RFC 3748 §5.2). The general notification code is a
// 16-bit value; bit 0x8000 distinguishes success/failure and bit 0x4000 the
// code class.
var notificationNames = map[uint16]string{
	0x0000: "general success",
	0x0001: "general failure",
	0x1000: "user account expired",
	0x1001: "user no longer valid",
	0x1002: "user not found",
}

// NotificationCodeToString renders an EAP notification code as a human-readable
// string.
func NotificationCodeToString(code uint16) string {
	if name, ok := notificationNames[code]; ok {
		return fmt.Sprintf("notification: %s", name)
	}
	class := "successful"
	if code&0x4000 != 0 {
		class = "failure"
	}
	kind := "no NAI requested"
	if code&0x8000 != 0 {
		kind = "NAI requested"
	}
	return fmt.Sprintf("notification (code %d, %s, %s)", code, class, kind)
}