//go:build linux

package device

import (
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/yibaiba/hideck/pkg/logger"
	"golang.org/x/sys/unix"
)

// udevKernelUeventGroup is the kernel kobject uevent multicast group.
const udevKernelUeventGroup = 1

const udevNetlinkRecvBuf = 1024 * 1024

// UdevWatcher 监听 USB 设备热插拔事件
type UdevWatcher struct {
	pool     *Pool
	stop     chan struct{}
	stopOnce sync.Once

	pendingMu      sync.Mutex
	waveKind       udevEventKind
	waveStart      time.Time
	waveSawControl bool
	waveGen        uint64
	quietTimer     *time.Timer
	maxTimer       *time.Timer
	firedAt        time.Time
	firedKind      udevEventKind
}

// NewUdevWatcher 创建 udev 监听器
func NewUdevWatcher(pool *Pool) *UdevWatcher {
	return &UdevWatcher{
		pool: pool,
		stop: make(chan struct{}),
	}
}

// Start 启动 udev 事件监听
func (w *UdevWatcher) Start() {
	go w.loop()
}

// Stop 停止监听
func (w *UdevWatcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stop)
		w.pendingMu.Lock()
		w.stopWaveLocked()
		w.pendingMu.Unlock()
	})
}

func (w *UdevWatcher) loop() {
	conn, err := openUdevNetlink()
	if err != nil {
		logger.Warn("udev 监听器启动失败，热插拔功能不可用", "err", err)
		return
	}
	defer conn.Close()

	logger.Info("udev 设备热插拔监听器已启动")

	buf := make([]byte, 8192)
	for {
		select {
		case <-w.stop:
			logger.Info("udev 监听器已停止")
			return
		default:
		}

		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
				continue
			}
			logger.Debug("udev netlink 读取失败", "err", err)
			continue
		}
		if n <= 0 {
			continue
		}
		// Payload is KEY=value; do not require a parsed nlmsghdr.
		payload := buf[:n]
		kind := parseUdevEventKind(payload)
		if kind != udevEventNone && w.isModemEvent(payload) {
			w.scheduleRescan(kind, udevEventHasControlPath(payload))
		}
	}
}

func openUdevNetlink() (*os.File, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_KOBJECT_UEVENT)
	if err != nil {
		return nil, err
	}
	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, udevNetlinkRecvBuf)
	sa := &unix.SockaddrNetlink{
		Family: unix.AF_NETLINK,
		Groups: udevKernelUeventGroup,
	}
	if err := unix.Bind(fd, sa); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), "kobject-uevent"), nil
}

// isModemEvent 检查是否是 USB 调制解调器相关事件
func (w *UdevWatcher) isModemEvent(data []byte) bool {
	s := string(data)

	// 检查 ACTION
	if !strings.Contains(s, "ACTION=add") && !strings.Contains(s, "ACTION=remove") {
		return false
	}

	// 检查 SUBSYSTEM（usb/net/tty/usbmisc/wwan 都可能是调制解调器相关）
	if strings.Contains(s, "SUBSYSTEM=usb") ||
		strings.Contains(s, "SUBSYSTEM=net") ||
		strings.Contains(s, "SUBSYSTEM=tty") ||
		strings.Contains(s, "SUBSYSTEM=usbmisc") ||
		strings.Contains(s, "SUBSYSTEM=wwan") {

		// 进一步过滤：排除无关设备
		// 如果是 net 子系统，只关心 wwan 开头的接口
		if strings.Contains(s, "SUBSYSTEM=net") {
			if !strings.Contains(s, "wwan") {
				return false
			}
		}

		// 如果是 tty 子系统，只关心 ttyUSB
		if strings.Contains(s, "SUBSYSTEM=tty") {
			if !strings.Contains(s, "ttyUSB") {
				return false
			}
		}

		logger.Debug("检测到调制解调器相关 udev 事件", "data_preview", truncateString(s, 200))
		return true
	}

	return false
}

// scheduleRescan 枚举会连着冒 ttyUSB，3s 重置会把识别拖到枚举结束之后。
func (w *UdevWatcher) scheduleRescan(kind udevEventKind, controlReady bool) {
	if kind == udevEventNone {
		return
	}
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()

	now := time.Now()
	if udevSwallowAfterFire(w.firedAt, now, w.firedKind, kind, controlReady) {
		return
	}
	if w.waveKind != udevEventNone && kind != w.waveKind {
		w.stopWaveLocked()
	}

	quiet, max := udevDebounceWindows(kind)
	if w.waveKind == udevEventNone {
		w.waveKind = kind
		w.waveStart = now
		w.waveSawControl = controlReady
		w.waveGen++
		gen := w.waveGen
		w.maxTimer = time.AfterFunc(max, func() { w.tryFireRescan(gen) })
		// add 在控制口出现前只靠上限，避免 tty 安静 400ms 就扫到空控制口。
		if kind == udevEventRemove || controlReady {
			w.quietTimer = time.AfterFunc(quiet, func() { w.tryFireRescan(gen) })
		}
		return
	}

	if controlReady {
		w.waveSawControl = true
	}
	if kind == udevEventAdd && !w.waveSawControl {
		return
	}
	if w.quietTimer != nil {
		w.quietTimer.Stop()
	}
	gen := w.waveGen
	w.quietTimer = time.AfterFunc(quiet, func() { w.tryFireRescan(gen) })
}

func (w *UdevWatcher) tryFireRescan(gen uint64) {
	w.pendingMu.Lock()
	if gen != w.waveGen || w.waveKind == udevEventNone {
		w.pendingMu.Unlock()
		return
	}
	w.firedAt = time.Now()
	w.firedKind = w.waveKind
	w.stopWaveLocked()
	w.pendingMu.Unlock()

	logger.Info("udev 检测到设备变化，执行重新扫描")
	if w.pool != nil {
		if woken := w.pool.WakeModemRebootRecoveries("udev_modem_event"); woken > 0 {
			logger.Debug("udev 事件已唤醒模组重启恢复流程", "recoveries", woken)
			return
		}
		if err := w.pool.RescanAndReconnect(); err != nil {
			logger.Warn("设备重新扫描失败", "err", err)
		}
	}
}

func (w *UdevWatcher) stopWaveLocked() {
	if w.quietTimer != nil {
		w.quietTimer.Stop()
		w.quietTimer = nil
	}
	if w.maxTimer != nil {
		w.maxTimer.Stop()
		w.maxTimer = nil
	}
	w.waveKind = udevEventNone
	w.waveStart = time.Time{}
	w.waveSawControl = false
}

// truncateString 截断字符串用于日志
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
