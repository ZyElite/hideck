# Web 命令中心与运营商余额查询实施方案

## 目标

在 VoHive Web 管理端增加独立的 `/commands` 页面，让管理员通过类似 QQBOT 的对话输入执行现有命令，并从当前可用 SIM 发起运营商余额查询。命令结果、执行过程和余额原始回复必须持久化，刷新或重启后仍可查看。

## 命令中心

- 暴露 `/help`、`/list`、`/status`、`/send`、`/sms`、`/esim`、`/switch`、`/vocall`、`/rotate`、`/balance`。
- Web 与 QQ、Telegram、飞书复用同一份命令注册和执行逻辑，避免两套行为。
- 完整输入命令后立即执行；快捷按钮执行 `/switch`、`/vocall`、`/rotate` 前必须确认。
- 命令执行记录和事件写入 SQLite，通过 SSE 实时更新，支持游标分页和清空历史。
- 单管理员部署共享一条全局时间线。

## 余额查询

- 统一支持 SMS 和 USSD 查询。VoWiFi 已启用时，SMS 只能走 VoWiFi，USSD 走 VoWiFi USSI；否则使用当前设备后端。
- 查询不得切换飞行模式、不得开启蜂窝网络、不得自动重试。
- 同一 ICCID 同时只允许一条待处理查询，等待短信回复最多五分钟。
- 结果展示结构化余额、币种和摘要，同时保留运营商原始回复。无法解析的真实回复按完成但未解析处理，不伪装失败或成功余额。
- 内置规则只读，数据库中的自定义规则可覆盖；自定义正则使用 Go RE2。
- 手工查询，不实现定时任务。

## 运营商覆盖

覆盖当前 16 个 MCC/MNC 预设：2degrees、AT&T 两个网号、CSL、CTExcel、giffgaff、O2 DE 两个网号、One NZ、Spark NZ、Sunrise、Three HK、Three UK、T-Mobile US 两个网号和 Vodafone NL。规则必须注明 SMS、USSD、官方替代方式、证据来源和适用限制；资料不足时明确不支持，禁止猜测。

## API

- `GET /api/command-center/commands`
- `POST /api/command-center/executions`
- `GET /api/command-center/events`
- `GET /api/command-center/stream`
- `DELETE /api/command-center/history`
- `GET /api/balances`
- `POST /api/devices/{device_id}/balance-queries`
- `GET /api/devices/{device_id}/balance-queries`
- `GET/POST /api/carrier-query-rules`
- `PUT/DELETE /api/carrier-query-rules/{id}`

全部复用现有 Bearer 鉴权。

## 前端

- 桌面端为左侧设备/余额栏、右侧命令时间线和输入区；移动端设备栏置顶、输入区固定在安全区域。
- 支持斜杠命令补全、设备参数选择、快捷动作、危险动作确认、加载/错误/空状态、SSE 断线续传。
- 使用现有 Element Plus、图标库和本地字体，不引入新依赖。

## 验证

- 后端单元和 HTTP 集成测试全部使用 `-timeout=60s`。
- 对 16 个运营商规则做表驱动覆盖；验证命令共用注册表、余额状态机、短信关联、USSD 路由、鉴权、SSE 续传和历史清理。
- 跑前端类型检查、测试和构建，并在桌面及移动视口完成真实浏览器交互与截图检查。
- 构建 Linux amd64 包，校验 SHA256 后部署到 `yibai@192.168.11.179:/home/yibai/vohive/vohive-open`，完成不产生短信/USSD 副作用的冒烟测试。
- 真实 CTExcel `BAL` 和 giffgaff `INFO` 查询需要执行时再次取得明确授权，不自动发送。

## 非目标

- 不实现多租户、聊天机器人自然语言理解或定时余额查询。
- 不为了余额查询临时开启蜂窝或改变卡策略。
- 不把无法验证的运营商规则标记为免费或可用。
