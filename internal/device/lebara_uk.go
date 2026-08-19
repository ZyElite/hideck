package device

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/yibaiba/hideck/internal/backend"
	"github.com/yibaiba/hideck/internal/db"
)

const (
	RFLockLebaraUKNextGen       = "lebara_uk_nextgen"
	lebaraUKLiveIdentityTimeout = 3 * time.Second
)

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
	return c.IsLebara && c.LiveFlipped
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
	liveFlipped := strings.HasPrefix(imsi, "20404")
	hasLebaraEvidence := profileNameLooksLikeLebaraUK(profileName) || hasIMSIPrefix(seenIMSIs, "23487")
	if liveHome || (hasLebaraEvidence && (imsi == "" || liveFlipped)) {
		class.IsLebara = true
	}
	class.LiveHome23487 = liveHome
	class.LiveFlipped = class.IsLebara && liveFlipped
	return class
}

func ClassifyWorkerLebaraUK(w *Worker) (LebaraUKClass, error) {
	if w == nil {
		return LebaraUKClass{}, nil
	}
	return classifyLebaraUKWithHistory(w.GetCachedIMSI(), workerLebaraUKProfileName(w), w.CurrentICCID())
}

// ClassifyWorkerLebaraUKForControl may read IMSI only while the identity cache is empty.
// The bounded read is reserved for operations that can enable radio or cellular data.
func ClassifyWorkerLebaraUKForControl(ctx context.Context, w *Worker) (LebaraUKClass, error) {
	if w == nil {
		return LebaraUKClass{}, nil
	}
	imsi := w.GetCachedIMSI()
	canReadLive := w.Backend != nil && backend.NormalizeBackendMode(w.Backend.Mode()) != backend.BackendAT
	if imsi == "" && canReadLive {
		if ctx == nil {
			ctx = context.Background()
		}
		liveCtx, cancel := context.WithTimeout(ctx, lebaraUKLiveIdentityTimeout)
		defer cancel()
		liveIMSI, err := w.Backend.GetIMSI(liveCtx)
		if err != nil {
			return LebaraUKClass{}, fmt.Errorf("读取实时 IMSI 失败: %w", err)
		}
		imsi = strings.TrimSpace(liveIMSI)
	}
	return classifyLebaraUKWithHistory(imsi, workerLebaraUKProfileName(w), w.CurrentICCID())
}

func ClassifyLebaraUKForICCID(iccid, profileName string) (LebaraUKClass, error) {
	return classifyLebaraUKWithHistory("", profileName, iccid)
}

func classifyLebaraUKWithHistory(imsi, profileName, iccid string) (LebaraUKClass, error) {
	class := ClassifyLebaraUKNextGen(imsi, profileName, nil)
	if class.IsLebara || !lebaraUKHistoryCanDisambiguate(imsi) {
		return class, nil
	}
	seenIMSIs, err := db.ListIMSIsForICCID(iccid)
	if err != nil {
		return LebaraUKClass{}, err
	}
	return ClassifyLebaraUKNextGen(imsi, profileName, seenIMSIs), nil
}

func lebaraUKHistoryCanDisambiguate(imsi string) bool {
	imsi = strings.TrimSpace(imsi)
	return imsi == "" || strings.HasPrefix(imsi, "20404")
}

func workerLebaraUKProfileName(w *Worker) string {
	if w == nil || w.EsimMgr == nil {
		return ""
	}
	name, _ := w.EsimMgr.ActiveProfileName()
	return name
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

func (p *Pool) guardLebaraUKRadioRestore(w *Worker, reason string) (bool, error) {
	class, err := ClassifyWorkerLebaraUK(w)
	if err != nil {
		if w != nil {
			w.setCellularRadioSuppressed(true)
		}
		if p != nil {
			p.enterAirplaneModeFromPolicy(w, reason)
		}
		return true, fmt.Errorf("识别 Lebara UK 射频策略失败: %w", err)
	}
	if !class.IsLebara {
		return false, nil
	}
	if p != nil {
		p.enforceLebaraUKRadioOff(w, reason)
	}
	return true, nil
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
