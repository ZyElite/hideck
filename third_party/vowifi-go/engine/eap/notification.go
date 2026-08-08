package eap

import "fmt"

var akaNotificationCodeTextsIANA = map[uint16]string{
	0:     "General failure after authentication (通用认证后失败)",
	1026:  "User has been temporarily denied access (用户被临时拒绝访问)",
	1031:  "User has not subscribed to the requested service (用户未订阅请求的服务)",
	16384: "General failure (通用失败)",
	16385: "Certificate replacement required (需要更换证书)",
	32768: "Success (成功)",
}

var akaNotificationCodeTexts3GPP = map[uint16]string{
	10500: "APN not subscribed (APN 未签约)",
	10501: "Authorization rejected (授权被拒绝)",
	11000: "Network failure (网络故障)",
	11001: "RAT type not allowed (接入技术类型不允许)",
	11002: "Tracking area not allowed (跟踪区域不允许)",
	11003: "Roaming not allowed (不允许漫游)",
	11004: "Identity cannot be resolved (身份无法解析)",
	11005: "Congestion (网络拥塞)",
	11011: "PLMN not allowed (PLMN 不允许)",
}

func NotificationCodeToString(code uint16) string {
	if description, ok := akaNotificationCodeTextsIANA[code]; ok {
		return fmt.Sprintf("[IANA] %s", description)
	}
	if description, ok := akaNotificationCodeTexts3GPP[code]; ok {
		return fmt.Sprintf("[3GPP] %s", description)
	}
	phase := "认证后"
	if code&0x4000 != 0 {
		phase = "认证前"
	}
	action := "需要 Success/Failure 结束"
	if code&0x8000 != 0 {
		action = "纯通知"
	}
	return fmt.Sprintf("未知通知码 %d (阶段: %s, 类型: %s)", code, phase, action)
}

func IsFailureNotificationCode(code uint16) bool {
	if code == 32768 {
		return false
	}
	if _, ok := akaNotificationCodeTextsIANA[code]; ok {
		return true
	}
	if _, ok := akaNotificationCodeTexts3GPP[code]; ok {
		return true
	}
	return code&0x8000 == 0
}
