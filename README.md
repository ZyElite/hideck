# VoHive

> Windloom source integration tree: this repository vendors the visible
> project-level VoHive source dependencies under `third_party/` and builds
> without the unavailable upstream `github.com/iniwex5/vowifi-go` repository.
> See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for source origins,
> license notes, and build-chain details.

## Local source build

```bash
npm ci --prefix web
npm run build --prefix web
rm -rf internal/web/dist
mkdir -p internal/web
cp -R web/dist internal/web/dist

GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -buildvcs=false -tags "with_utls nomsgpack" \
  -o dist/vohive-open_linux_amd64 ./cmd/vohive

GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -trimpath -buildvcs=false -tags "with_utls nomsgpack" \
  -o dist/vohive-open_linux_arm64 ./cmd/vohive
```

## Docker

```bash
mkdir -p vohive/{config,data,logs}
cp config/config.example.yaml vohive/config/config.yaml
cd vohive
```

Use the compose file from this repository, or create one with:

```yaml
services:
  vohive:
    image: skyhotspur/vohive:${VOHIVE_TAG:-1.5.5}
    container_name: vohive
    restart: unless-stopped
    network_mode: host
    privileged: true
    volumes:
      - ./config:/app/config
      - ./data:/app/data
      - ./logs:/app/logs
      - /dev:/dev
    environment:
      TZ: Asia/Shanghai
      CONFIG_PATH: /app/config/config.yaml
```

Default login: `admin` / `admin`. Change the password after first login. Set
`VOHIVE_TAG` before running Compose when you want a specific image tag.

## EC25 SIM 热插拔设置

EC25 的 USB/QMI 设备热插拔与实体 SIM 卡热插拔是两个独立功能。若模组已被
VoHive 识别，但运行中插入 SIM 后没有状态变化，请先通过模组的 AT 端口查询：

```text
AT+QSIMDET?
AT+QSIMSTAT?
```

`QSIMDET: 0,0` 表示实体 SIM 热插拔检测未启用。对于“SIM 插入时检测引脚为低电平”
的卡座，可执行：

```text
AT+QSIMDET=1,0
AT+QSIMSTAT=1
AT&W
AT+CFUN=1,1
```

如果卡座在 SIM 插入时检测引脚为高电平，应使用 `AT+QSIMDET=1,1`。检测电平必须
与硬件设计一致，否则热插拔功能无效。`QSIMDET` 需要重启模组后生效，`QSIMSTAT=1`
用于启用 SIM 插入/移除状态上报，`AT&W` 用于保存该上报设置。

重启后可再次执行 `AT+QSIMSTAT?` 验证。`+QSIMSTAT: 1,1` 表示已检测到 SIM，
`+QSIMSTAT: 1,0` 表示模组仍判断 SIM 已移除。还可通过 `AT+CPIN?` 和 `AT+QCCID`
确认 SIM 是否完成初始化以及能否读取 ICCID。具体参数含义参见
[Quectel EC25 & EC21 AT Commands Manual](https://quectel.com/content/uploads/2021/03/Quectel_EC25EC21_AT_Commands_Manual_V1.3.pdf)。

### 大疆定制模块恢复为移远 EC25 USB 身份

以下步骤适用于 USB 当前识别为 `2ca3:4006`、且已确认底层为移远 EC25 的大疆定制
模块。该操作只修改 USB VID/PID 与接口组合，不会修改 IMEI。`AT+QCFG="usbcfg"`
会自动保存配置，并在模组重启后生效；错误的接口参数可能导致 AT 口或网络接口消失，
不要用于其他型号的模组。

在虚拟机中依次执行：

```bash
# 0. 安装 socat（用于发送 AT 指令）
sudo apt-get update && sudo apt-get install socat -y

# 1. 临时加载 option 串口驱动
sudo modprobe option

# 2. 将大疆当前 USB ID 注册到 option 驱动，生成 /dev/ttyUSB* 串口
echo 2ca3 4006 | sudo tee /sys/bus/usb-serial/drivers/option1/new_id

# 3. 确认 /dev/ttyUSB2 是 AT 口后，将 USB 身份永久改为移远 2c7c:0125
echo 'AT+QCFG="usbcfg",0x2C7C,0x0125,1,1,1,1,1,0,0' | socat - /dev/ttyUSB2,crnl

# 4. 软重启模组，使新 USB 配置生效
echo 'AT+CFUN=1,1' | socat - /dev/ttyUSB2,crnl
```

软重启会让 `/dev/ttyUSB*` 和网络接口暂时消失。等待模组重新枚举后检查：

```bash
lsusb
```

预期结果包含：

```text
2c7c:0125 Quectel Wireless Solutions Co., Ltd. EC25 LTE modem
```

若 `/dev/ttyUSB2` 不存在或不是 AT 口，应先根据实际枚举结果确认端口，不能直接发送
持久化配置。当前用户也必须拥有该串口的读写权限。`usbcfg` 参数含义参见
[Quectel EC2x/EG2x/EG9x/EM05 QCFG AT Commands Manual](https://quectel.com/content/uploads/2024/02/Quectel_EC2xEG2xEG9xEM05_Series_QCFG_AT_Commands_Manual_V1.0.pdf)。

[![License: PolyForm Noncommercial 1.0.0](https://img.shields.io/badge/License-PolyForm--Noncommercial--1.0.0-blue.svg)](https://polyformproject.org/licenses/noncommercial/1.0.0)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go)](go.mod)
[![Vue 3](https://img.shields.io/badge/Vue-3-42b883?logo=vue.js)](web/package.json)

> 面向高通 4G/LTE/5G 模组（Quectel EC20/EC25/EC21/EG25/EM20 等）的综合管理与代理服务平台。

VoHive 把模组热插拔管理、SOCKS5/HTTP 代理编排、短信收发、VoWiFi/IMS 通话、eSIM 全生命周期管理整合到一个服务里,并提供一套现代化的响应式 Web 管理后台。

## 核心特性

| 模块 | 说明 |
| --- | --- |
| 多模组并发管理 | USB 热插拔自动发现(ttyUSB 等)、多设备实时状态监控 |
| 轻量级代理引擎 | 内建 SOCKS5 / HTTP 代理内核,支持多实例并发;基于 `SO_BINDTODEVICE` 按设备网卡严格绑定出站流量 |
| 通信与短信中心 | 统一界面/API 处理 AT 短信收发、会话与联系人管理、USSD 交互,短信落库可查 |
| eSIM 管理 | 通过 AT 指令通道直接管理 eSIM 芯片,支持 Profile 下载、启用/停用、重命名、删除 |
| 全渠道通知 | 重要短信及系统告警可推送至 Telegram、Email、PushPlus、Bark、飞书(Lark/Feishu)、QQ 等 |
| 多架构构建 | 原生支持 amd64 / arm64 / arm7 跨平台编译,路由器到边缘节点均可部署 |

## 典型应用场景

- **私有 IP 代理池**:单主机挂载多张物理 SIM 卡或多张 eSIM,每张网卡对应独立的 SOCKS5/HTTP 实例,组建自己的移动网络代理。
- **统一接码/验证码中心**:Web 界面或 API 并行收发多卡短信,并通过 Webhook/Bot 实时推送到个人终端。
- **VoWiFi 零信号通信**:地下室、弱覆盖场景下,借助宽带网络隧道建立 IMS 连接,保证业务不掉线。

## 架构与技术栈

- **Backend**:Go 1.26+(Gin、GORM、Viper、euicc-go)
- **Frontend**:Vue 3 + Vite + TailwindCSS + Element Plus
- **Database**:SQLite(`vohive.db`)
- **CI/CD**:GitHub Actions 自动化多架构 Docker 镜像构建与发布


## 免责声明

- **用途定位**:本项目主要面向个人学习、技术研究与功能测试场景,不建议直接用于生产环境或关键业务系统;由此产生的部署及使用风险由使用者自行承担。
- **非官方项目**:VoHive 为第三方独立开发的开源软件,与 Quectel(高通模组厂商)、高通公司及其他任何模组/芯片厂商均无官方关联、授权或合作关系,亦不对模组硬件本身的功能、质量或安全性负责。
- **合规使用**:使用本项目搭建的服务时,请自行确保符合所在地区的法律法规及电信运营商的服务条款,不得用于任何违法违规用途。因违规使用造成的一切法律责任由使用者自行承担,与本项目作者及贡献者无关。
- **无担保**:本软件按"现状"提供,不附带任何明示或暗示的担保,包括但不限于适销性、特定用途适用性及不侵权担保。因使用或无法使用本软件(含数据丢失、设备异常、业务中断等)造成的任何直接或间接损失,作者及贡献者不承担任何责任。

## License

本源码整合树不是单一许可项目。根项目基于 [PolyForm Noncommercial License 1.0.0](LICENSE)，`third_party/vowifi-go` 使用 AGPL-3.0，`third_party/quectel-qmi-go`、`third_party/netlink`、`third_party/qqbot` 等组件按各自许可证授权。发布公开二进制或 Docker 镜像前，请先确认组合分发的许可证义务；详情见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
