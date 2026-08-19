package device

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/yibaiba/hideck/internal/db"
)

const RFLockLebaraUKNextGen = "lebara_uk_nextgen"

var (
	// ErrLebaraUKRFLocked 是分享卡射频锁：不能关飞行、开网络或切蜂窝。
	ErrLebaraUKRFLocked = errors.New("Lebara UK 分享卡不能驻国内网或开流量，否则 IMSI 会切到 20404，WiFi calling 会废")
	// ErrLebaraUKFlippedIMSI 是活 IMSI 已离开 23487 时拒绝 WiFi calling。
	ErrLebaraUKFlippedIMSI = errors.New("Lebara UK IMSI 已切到 20404，英国 WiFi calling 不可用，请保持飞行")

	lebaraUKProfileName = regexp.MustCompile(`(?i)(?:^|[\s\-_])(?:\d+\s+)?lebara\s*uk(?:$|[\s\-_])`)
)

// LebaraUKClass 是 234/87 NextGen 分享卡的识别结果。活 IMSI 20404 本身不是 Lebara。
type LebaraUKClass struct {
	IsLebara      bool
	LiveHome23487 bool
	LiveFlipped   bool
	LiveIMSI      string
}

func (c LebaraUKClass) RFLock() string {
	if c.IsLebara {
		return RFLockLebaraUKNextGen
	}
	return ""
}

func (c LebaraUKClass) BlocksVoWiFi() bool {
	return c.IsLebara && !c.LiveHome23487 && strings.TrimSpace(c.LiveIMSI) != ""
}

func NewLebaraUKFlippedIMSIError(imsi string) error {
	imsi = strings.TrimSpace(imsi)
	if imsi == "" {
		return ErrLebaraUKFlippedIMSI
	}
	return fmt.Errorf("%w（当前 %s）", ErrLebaraUKFlippedIMSI, imsi)
}

func IsLebaraUKPolicyError(err error) bool {
	return errors.Is(err, ErrLebaraUKRFLocked) || errors.Is(err, ErrLebaraUKFlippedIMSI)
}

// ClassifyLebaraUKNextGen 按活 IMSI、eSIM 档名、同 ICCID 历史 IMSI 判定。
// 不要用 GID，也不要把光秃的 20404 当成 Lebara。
func ClassifyLebaraUKNextGen(imsi, profileName string, seenIMSIs []string) LebaraUKClass {
	imsi = strings.TrimSpace(imsi)
	class := LebaraUKClass{LiveIMSI: imsi}
	liveHome := strings.HasPrefix(imsi, "23487")
	if liveHome || profileNameLooksLikeLebaraUK(profileName) || hasIMSIPrefix(seenIMSIs, "23487") {
		class.IsLebara = true
	}
	class.LiveHome23487 = liveHome
	class.LiveFlipped = class.IsLebara && strings.HasPrefix(imsi, "20404")
	return class
}

func ClassifyWorkerLebaraUK(w *Worker) LebaraUKClass {
	if w == nil {
		return LebaraUKClass{}
	}
	imsi := strings.TrimSpace(w.GetIMSI())
	if imsi == "" {
		imsi = w.GetCachedIMSI()
	}
	name := ""
	if w.EsimMgr != nil {
		name, _ = w.EsimMgr.ActiveProfileName()
	}
	return ClassifyLebaraUKNextGen(imsi, name, db.ListIMSIsForICCID(w.CurrentICCID()))
}

func applyLebaraUKRFLock(w *Worker) {
	if w == nil {
		return
	}
	w.Config.AirplaneEnabled = true
	w.Config.NetworkEnabled = false
	w.Config.PhoneMode = "wifi"
	w.restoreNetworkAfterVoWiFi = false
	w.setCellularRadioSuppressed(true)
}

func profileNameLooksLikeLebaraUK(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if lebaraUKProfileName.MatchString(" " + name + " ") {
		return true
	}
	compact := strings.ToLower(strings.Join(strings.Fields(name), " "))
	return compact == "lebara uk" || strings.HasPrefix(compact, "lebara uk ")
}

func hasIMSIPrefix(imsis []string, prefix string) bool {
	for _, imsi := range imsis {
		if strings.HasPrefix(strings.TrimSpace(imsi), prefix) {
			return true
		}
	}
	return false
}
