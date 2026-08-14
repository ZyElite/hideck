package device

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/yibaiba/hideck/pkg/logger"
	"github.com/yibaiba/hideck/pkg/mbim"
)

const (
	mbimProxyAbstractSocket        = "@mbim-proxy"
	mbimIdentityMaxControlTransfer = 4096
	mbimIdentityProbeTimeout       = 45 * time.Second
)

// ProbeIMEIViaMBIM 通过 MBIM DeviceCaps 探测设备 IMEI。
// 适用于 cdc_mbim 驱动的设备，替代 QMI 协议探测。
//
// 经 mbim-proxy 打开(transport=auto，proxy 优先),而不是 direct:direct 直接抢占
// 控制口的单一 OPEN 会话,与已持有该口的一方(ModemManager / hideck 自身 worker)
// 串话,读回垃圾(EM7430 上表现为 934 字节乱码)。mbim-proxy 独占并串行化该会话,
// 等价于 `mbimcli -p`,能稳定取到 DeviceCaps。
func ProbeIMEIViaMBIM(controlPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mbimIdentityProbeTimeout)
	defer cancel()
	return ProbeIMEIViaMBIMContext(ctx, controlPath)
}

// ProbeIMEIViaMBIMContext 继承调用方生命周期，避免重枚举后继续占用失效控制口。
func ProbeIMEIViaMBIMContext(ctx context.Context, controlPath string) (string, error) {
	controlPath = strings.TrimSpace(controlPath)
	if controlPath == "" {
		return "", fmt.Errorf("MBIM control path is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	ensureMBIMProxyRunningContext(ctx)

	transport, err := openMBIMIdentityTransport(ctx, controlPath)
	if err != nil {
		return "", fmt.Errorf("打开 MBIM 设备 %s 失败: %w", controlPath, err)
	}
	dev := mbim.NewDevice(transport)
	if err := dev.Open(ctx, mbimIdentityMaxControlTransfer); err != nil {
		_ = dev.Close()
		return "", fmt.Errorf("打开 MBIM 设备 %s 失败: %w", controlPath, err)
	}
	defer dev.Close()

	caps, err := mbim.DeviceCaps(ctx, dev)
	if err != nil {
		return "", fmt.Errorf("MBIM DeviceCaps 获取 IMEI 失败: %w", err)
	}

	imei := strings.TrimSpace(caps.DeviceID)
	if imei == "" {
		return "", fmt.Errorf("MBIM DeviceCaps 返回的 DeviceID 为空")
	}

	logger.Debug("MBIM IMEI 探测成功", "control_path", controlPath, "imei", imei)
	return imei, nil
}

type mbimIdentityDialResult struct {
	transport mbim.Transport
	err       error
}

func openMBIMIdentityTransport(ctx context.Context, controlPath string) (mbim.Transport, error) {
	result := make(chan mbimIdentityDialResult, 1)
	go func() {
		transport, err := mbim.Dial("auto", controlPath)
		result <- mbimIdentityDialResult{transport: transport, err: err}
	}()
	select {
	case opened := <-result:
		return opened.transport, opened.err
	case <-ctx.Done():
		go closeLateMBIMIdentityTransport(result)
		return nil, ctx.Err()
	}
}

func closeLateMBIMIdentityTransport(result <-chan mbimIdentityDialResult) {
	opened := <-result
	if opened.transport != nil {
		_ = opened.transport.Close()
	}
}

// mbimProxyCandidatePaths 是 mbim-proxy 二进制的查找顺序。libmbim 在多数发行版把它
// 装在 /usr/libexec(Debian 即如此),通常**不在 PATH** 里,故不能只靠裸命令名。
var mbimProxyCandidatePaths = []string{
	"/usr/libexec/mbim-proxy",
	"/usr/lib/mbim-proxy",
	"/usr/sbin/mbim-proxy",
	"/usr/bin/mbim-proxy",
}

// resolveMBIMProxyBinary 先查 PATH,再回退到已知安装位置;找不到返回空串。
func resolveMBIMProxyBinary() string {
	if p, err := exec.LookPath("mbim-proxy"); err == nil {
		return p
	}
	for _, p := range mbimProxyCandidatePaths {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// ensureMBIMProxyRunning 尽力保证 mbim-proxy 守护进程在运行(等价于 mbimcli 的 -p):
// 抽象套接字已就绪则直接返回;否则后台拉起 mbim-proxy 并等待套接字可用。
// 任何失败只记日志——调用方用 transport=auto,会自动回退到 direct,不阻断探测。
func ensureMBIMProxyRunningContext(ctx context.Context) {
	if mbimProxySocketReady() {
		return
	}
	bin := resolveMBIMProxyBinary()
	if bin == "" {
		logger.Debug("未找到 mbim-proxy 二进制(PATH 及已知位置均无),MBIM IMEI 探测将回退 direct")
		return
	}
	cmd := exec.Command(bin)
	if err := cmd.Start(); err != nil {
		logger.Debug("拉起 mbim-proxy 失败,MBIM IMEI 探测将回退 direct", "bin", bin, "err", err)
		return
	}
	// 让它脱离本次探测的生命周期,作为常驻守护进程(空闲后自行退出)。
	go func() { _ = cmd.Wait() }()
	for i := 0; i < 20; i++ {
		if mbimProxySocketReady() {
			return
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	logger.Debug("等待 mbim-proxy 抽象套接字就绪超时,MBIM IMEI 探测将回退 direct")
}

func mbimProxySocketReady() bool {
	conn, err := net.Dial("unix", mbimProxyAbstractSocket)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
