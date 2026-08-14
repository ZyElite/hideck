# HiDeck

[![License: PolyForm Noncommercial 1.0.0](https://img.shields.io/badge/License-PolyForm--Noncommercial--1.0.0-blue.svg)](https://polyformproject.org/licenses/noncommercial/1.0.0)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go)](go.mod)
[![Vue 3](https://img.shields.io/badge/Vue-3-42b883?logo=vue.js)](web/package.json)

HiDeck 是面向高通 4G/LTE/5G 模组的综合管理平台，将设备热插拔、移动网络代理、短信、VoWiFi/IMS 通话、eSIM 和自动任务整合在一个响应式 Web 控制台中。

- 项目仓库：[github.com/yibaiba/hideck](https://github.com/yibaiba/hideck)
- 默认 Web 端口：`7575`
- 默认账号：`admin` / `admin`
- 数据库：SQLite，默认路径 `data/hideck.db`

> 首次使用默认、明文或弱密码登录后，HiDeck 会提醒立即修改。通过网页修改的密码会以 bcrypt 哈希写回 `config.yaml`。HiDeck 会把免责声明同意状态写入数据库；只要持久化 `data/` 目录，同一实例换浏览器后不会重复提示。

## 核心能力

| 模块 | 能力 |
| --- | --- |
| 设备管理 | 自动发现 USB 模组，管理 QMI、MBIM、AT 与 PC/SC 后端，实时展示设备和网络状态 |
| 移动代理 | 为指定数据网卡创建 SOCKS5 / HTTP 出口，并通过 `SO_BINDTODEVICE` 绑定流量 |
| 短信与命令 | 收发短信、管理联系人和会话、执行 AT/USSD 命令、查询余额并保存历史 |
| VoWiFi / IMS | 建立 SWu/IMS 连接，处理短信与通话，并保存通话记录和录音 |
| eSIM | 下载、启用、停用、重命名和删除 eSIM Profile |
| 自动任务 | 按设备、Profile、时区和计划执行任务，记录运行历史和错误 |
| 通知 | 支持 Telegram、Email、PushPlus、Bark、飞书、企业微信、微信和 QQ 等渠道 |
| 多架构交付 | 支持 Linux amd64、arm64 与 armv7 构建及 Docker 部署 |

## Docker 快速部署

运行环境需要 Linux、Docker Compose、host 网络和 USB 设备访问权限。
服务器使用 `docker-compose.yml` 拉取发布镜像；维护者本机的多架构构建使用独立的 `docker-compose.build.yml`。

服务器可直接通过 curl 安装到当前目录下的 `hideck/`：

```bash
curl -fsSL https://raw.githubusercontent.com/yibaiba/hideck/main/deploy.sh | sh
```

自定义安装目录：

```bash
curl -fsSL https://raw.githubusercontent.com/yibaiba/hideck/main/deploy.sh | HIDECK_DIR=/opt/hideck sh
```

`deploy.sh` 会下载部署所需的 Compose 和配置模板，在首次运行时生成 `config/config.yaml`，创建持久化目录，拉取 `latest` 并启动容器。已有文件和配置不会被覆盖。在源码项目目录内也可以直接执行 `./deploy.sh`。

浏览器打开：

```text
http://YOUR_IP:7575
```

默认 Compose 配置使用：

- 镜像：`yibaiba/hideck:latest`
- 网络：`host`
- 设备权限：`privileged: true` 并挂载 `/dev`
- 持久化目录：`config/`、`data/`、`logs/`

查看状态和日志：

```bash
docker compose ps
docker compose logs -f hideck
```

## 配置

主配置文件为 `config/config.yaml`，可从 [config/config.example.yaml](config/config.example.yaml) 复制。常用配置如下：

| 配置 | 默认值 | 说明 |
| --- | --- | --- |
| `server.port` | `7575` | HTTP 管理端口 |
| `server.https_port` | `7576` | HTTPS 管理端口 |
| `web.username` | `admin` | Web 登录账号 |
| `web.password` | `admin` | Web 登录密码，登录后应立即修改 |
| `system.openwrt_dynamic_interfaces` | `false` | 仅在 OpenWrt 上启用动态接口映射 |
| `vowifi.enabled` | `false` | 全局 VoWiFi 开关 |

不要把 SIM PIN、Bot Token、API Key 或其他凭据直接提交到配置仓库。SIM PIN 配置只保存环境变量名，例如 `HIDECK_SIM_PIN_READER1`。

可以使用 `PROXY_WEB_PASSWORD` 注入 Web 密码。环境变量优先于 `config.yaml`，因此控制台不会尝试覆盖它：若注入的是弱密码，服务仍会启动，但启动日志和登录页都会明确警告；请在 Docker Compose、systemd 或启动脚本中修改该变量并重启服务。自定义强密码不会重复提示。

### Telegram、微信、企业微信与 QQ 通知

设置页的“通知”区域把 Telegram Bot、个人微信、企业微信长连接机器人、企业微信 Webhook 和 QQ Bot 分开管理：

- Telegram：通过 `@BotFather` 的 `/newbot` 创建机器人并取得 Bot Token。填写管理员的 Telegram 数字用户 ID；通知 Chat ID 可留空，第一个已授权管理员私聊命令会自动绑定为默认通知目标。Telegram Bot API 使用 Token 接入，不提供机器人扫码注册。
- 个人微信：在“个人微信”页签点击“扫码连接”。扫码确认后启用 iLink 通道；第一个合法私聊会成为默认通知目标。
- 企业微信长连接：在“企微机器人”页签扫码创建机器人，也可以手工填写 Bot ID 和 Secret。第一个合法私聊会自动绑定，群聊不能抢占首次绑定。
- QQ Bot：在“QQ Bot”页签扫码注册，也可以手工填写 App ID、App Secret 和目标 OpenID。扫码用户会自动成为管理员、私聊白名单和默认通知目标。
- 企业微信 Webhook：继续在独立的“企微 Webhook”页签配置，只负责推送，不提供双向命令或扫码登录。

扫码服务不可用时，页面会显示真实错误并保留手工配置入口，不会伪造连接成功。Telegram 使用 `admin_id` 限制私聊命令来源；个人微信和企业微信可用 `allowed_user_ids`、`allowed_group_ids`；QQ 使用 `group_ids`、`direct_ids`。Bot Token、扫码凭证和自动绑定状态涉及敏感数据，`config/config.yaml` 与同目录的 `notification-state.json` 都不应提交或共享。

连接后的人工验证步骤：

1. Telegram 先从管理员私聊发送 `/start`，其他双向渠道发送 `/help`；回复顶部应列出实时设备 ID，后续命令必须使用这里显示的 ID。Telegram 未显式配置 Chat ID 时，设置页刷新后应显示这个私聊的数字 ID。
2. 发送 `/status <设备ID>`，确认普通文本命令可以双向收发。
3. 发送 `/vocall <设备ID> <号码> [保持秒数]`。Telegram、个人微信和 QQ 应收到真实 MP3 录音；企业微信对不超过 2 MiB 的 AMR-NB 发送语音，AMR-WB、MP3 或超限录音会按文件发送并说明原因。
4. 触发一条短信或来电通知，确认已绑定的默认私聊能够收到通知。
5. 从不在白名单中的私聊或群聊发送命令，确认请求被拒绝；Telegram、个人微信和企业微信群聊不能用于首次绑定。

仓库自动测试覆盖扫码状态机、凭证加密/脱敏、配置持久化、长连接协议和本地媒体上传流程。真实 Telegram、微信、企业微信和 QQ 客户端仍需使用各自账号按上述步骤验收。

## 源码构建

### 依赖

- Go `1.26.4` 或兼容的 `1.26+` 版本
- Node.js 与 npm
- UPX（使用 Makefile 构建压缩发布包时需要）

### 使用 Makefile

```bash
make build-amd64
make build-arm64
make build-armv7
# 或一次构建全部架构
make build-all
```

Makefile 会先安装前端依赖、构建 Vue 应用并同步到 Go 嵌入目录，然后在 `dist/` 中生成 `hideck_*` 二进制。

### 不使用 UPX 的直接构建

```bash
npm ci --prefix web
npm run build --prefix web
rm -rf internal/web/dist
mkdir -p internal/web
cp -R web/dist internal/web/dist

GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -buildvcs=false -tags "with_utls nomsgpack" \
  -o dist/hideck_linux_amd64 ./cmd/hideck

GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -trimpath -buildvcs=false -tags "with_utls nomsgpack" \
  -o dist/hideck_linux_arm64 ./cmd/hideck
```

## 开发与验证

前端：

```bash
npm ci --prefix web
npm test --prefix web
npm run typecheck --prefix web
npm run lint --prefix web
npm run build --prefix web
```

后端：

```bash
go test -timeout=60s ./cmd/... ./internal/... ./pkg/...
go build ./cmd/hideck
```

开发环境可以通过 `web/.env.local` 设置 Vite 代理目标：

```dotenv
VITE_API_PROXY_TARGET=http://127.0.0.1:7575
```

服务启动后可访问 `/api/docs` 查看 OpenAPI 页面。

## 架构与技术栈

- 后端：Go、Gin、GORM、Viper
- 前端：Vue 3、Vite、Pinia、Element Plus、ECharts
- 数据库：SQLite
- 实时通信：SSE、WebSocket、WebRTC
- 交付：Docker、GitHub Actions、多架构 Linux 二进制

生产入口位于 `cmd/hideck`，前端源码位于 `web/`，主要业务模块位于 `internal/`，本地整合的上游源码位于 `third_party/`。

## EC25 与 SIM 检测

EC25 的 USB/QMI 热插拔和实体 SIM 检测是两个独立功能。大疆定制模块实测应关闭实体 SIM 热插拔检测：

```text
AT+QSIMDET=0,0
AT+CFUN=1,1
```

`QSIMDET=0,0` 会关闭基于 `SIM_DET` 引脚的检测。请在模组断电时插好 SIM 后重新上电，也可以在插卡后执行 `AT+CFUN=1,1` 软重启。

重启后通过 AT 端口验证：

```text
AT+QSIMDET?
AT+CPIN?
AT+QCCID
```

预期 `AT+QSIMDET?` 返回 `+QSIMDET: 0,0`，`AT+CPIN?` 返回 `+CPIN: READY`，并且 `AT+QCCID` 能读取 ICCID。参数说明参见 [Quectel EC25 & EC21 AT Commands Manual](https://quectel.com/content/uploads/2021/03/Quectel_EC25EC21_AT_Commands_Manual_V1.3.pdf)。

### 大疆定制模块恢复为移远 USB 身份

以下步骤仅适用于 USB 当前识别为 `2ca3:4006`、且已经确认底层为移远 EC25 的大疆定制模块。该操作会持久化 USB VID/PID 和接口组合；错误参数可能导致 AT 口或网络接口消失，不要用于其他型号。

```bash
sudo apt-get update && sudo apt-get install socat -y
sudo modprobe option

echo 2ca3 4006 | sudo tee /sys/bus/usb-serial/drivers/option1/new_id
echo 'AT+QCFG="usbcfg",0x2C7C,0x0125,1,1,1,1,1,0,0' | socat - /dev/ttyUSB2,crnl
echo 'AT+CFUN=1,1' | socat - /dev/ttyUSB2,crnl
```

等待重新枚举后运行 `lsusb`，预期包含：

```text
2c7c:0125 Quectel Wireless Solutions Co., Ltd. EC25 LTE modem
```

如果 `/dev/ttyUSB2` 不存在或不是 AT 口，必须先确认实际端口，不能直接发送持久化配置。当前用户也必须拥有串口读写权限。参数说明参见 [Quectel EC2x/EG2x/EG9x/EM05 QCFG AT Commands Manual](https://quectel.com/content/uploads/2024/02/Quectel_EC2xEG2xEG9xEM05_Series_QCFG_AT_Commands_Manual_V1.0.pdf)。

## AT、QMI 与 MBIM

| 通道 | Linux 常见节点/驱动 | 用途 |
| --- | --- | --- |
| AT | `/dev/ttyUSB*`、`/dev/ttyACM*` | 模组配置、诊断、短信和人工命令 |
| QMI | `/dev/cdc-wdm*` + `qmi_wwan` | 高通模组控制面、SIM、短信和蜂窝数据拨号 |
| MBIM | `/dev/cdc-wdm*` + `cdc_mbim` | USB-IF 标准化控制面和蜂窝数据拨号 |

同一块模组可以保留 AT 串口，同时把网络控制组合配置为 QMI 或 MBIM。HiDeck 在 QMI 模式管理控制面和数据面；在 MBIM 模式管理网络，短信和人工 AT 命令仍由同一个 AT 调度器串行执行。

MBIM 支持取决于具体硬件和固件的 USB 组合，不能只看产品系列或 `AT+QCFG="usbnet"` 是否接受参数。切换后至少确认：

1. 网卡由 `cdc_mbim` 驱动并出现 `/dev/cdc-wdm*`；
2. 标准 MBIM `OPEN` 能收到 `OPEN_DONE`；
3. `DeviceCaps` 能返回当前模组 IMEI。

只出现 `cdc_mbim` 接口不足以证明协议可用。当前测试的大疆定制 EC25 在 `usbnet=2` 时可以枚举 `cdc_mbim`，但标准 `OPEN` 无响应，因此 HiDeck 会拒绝持久化 MBIM 并保留或恢复 QMI 配置。其他型号必须逐台验证。

## 使用与许可

- HiDeck 仅用于个人学习、技术研究和功能测试，不建议直接用于生产或关键业务。
- HiDeck 是第三方独立项目，与 Quectel、高通及其他模组或芯片厂商没有官方关联、授权或合作关系。
- 使用者必须遵守所在地法律法规和电信运营商服务条款，不得用于违法违规用途。
- 软件按“现状”提供，不附带明示或暗示担保；使用风险由使用者自行承担。

本仓库是源码整合树，不是单一许可项目。根项目采用 [PolyForm Noncommercial License 1.0.0](LICENSE)；`third_party/vowifi-go` 使用 AGPL-3.0；其他第三方组件按各自许可证授权。公开分发二进制或 Docker 镜像前，请先核对组合分发义务，详情见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
