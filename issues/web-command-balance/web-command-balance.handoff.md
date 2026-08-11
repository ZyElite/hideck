# Web 命令中心与余额查询施工交工单

独立性：最终代码与证据由独立只读 reviewer 复核；reviewer 没有修改文件、连接远端或触发运营商动作。

## 总结

本轮按 `source.md` 实现了 Web 命令中心和手工运营商余额查询。十个斜杠命令、SQLite 时间线、SSE、余额状态机、16 个运营商预设、Bearer API、响应式页面和远端部署均已接通。代码与远端无副作用冒烟已经通过；唯一未完成的是 Chrome 真实桌面、手机和横屏交互验收，因为当前浏览器控制通道没有可连接实例。

## spec 目标逐条对账

| spec 目标 | 状态 | 实际效果 | 备注 |
|---|---|---|---|
| Web 与 QQ、Telegram、飞书共用十个斜杠命令 | 完成 | Web 和通知渠道调用同一组命令处理器 | `/help` 到 `/balance` 都来自共享目录 |
| 命令过程和结果持久化，支持分页、清空和 SSE 续传 | 完成 | 刷新后仍能查看历史，断线可按事件 ID 继续 | 清空只删除已结束记录 |
| SMS/USSD 严格沿当前 VoWiFi 或设备后端发送 | 完成，未做运营商实发 | VoWiFi 活跃时只走 VoWiFi SMS/USSI，否则走当前后端 | 不切飞行模式、不打开蜂窝、不自动重试 |
| 同一 ICCID 只保留一条待处理查询，五分钟内关联完整回复 | 完成 | 并发请求会冲突，完整短信按 ICCID、时间和发送者关联 | 空回复发送者规则会在保存前被拒绝 |
| 同时保存结构化余额和运营商原文 | 完成 | 页面显示金额/币种/摘要，也保留原始回复 | 解析失败显示“已收到但未解析” |
| 覆盖 16 个运营商预设，资料不足时明确不支持 | 完成 | 所有目标 MCC/MNC 都有规则或替代说明 | CTExcel 为历史项目观察，资费状态未知 |
| 内置规则只读，自定义规则可增删改并使用 Go RE2 | 完成 | 自定义规则可覆盖内置规则，非法正则和空发送者会报错 | 内置规则不会写入数据库 |
| 提供独立、响应式的命令中心页面 | 部分验证 | 页面有补全、设备选择、危险确认、实时事件、左侧余额栏和移动安全区 | 缺真实浏览器三视口点击与截图证据 |
| 所有新 API 使用 Bearer 鉴权并写入 OpenAPI | 完成 | 批准的 12 个方法/路径未鉴权均返回 401 | 旧 API 作为同鉴权处理器的兼容别名保留 |
| 构建 Linux amd64 包并完成远端无副作用冒烟 | 完成 | 最终包已运行，systemd、API、SSE、设备和日志均正常 | 两设备 VoWiFi 五阶段就绪，蜂窝网络关闭 |
| 未经再次授权不发送 CTExcel/giffgaff 查询 | 完成 | 本轮只执行 `/help` 和读取型接口 | 没有 SMS、USSD/USSI 或拨号 |

## 施工细节

### 共享命令和时间线

`internal/notify/command_service.go` 现在是 Web 与通知渠道的共同命令注册表。Web 请求先创建 running 执行和 accepted 事件，再调用共享处理器；结果写入 SQLite 并推送给 SSE。

```text
Web /commands -> Bearer API -> commandcenter.Service -> notify.CommandService
                                      |                        |
                                      v                        v
                                 SQLite 事件              现有设备动作
                                      |
                                      v
                                  SSE 时间线
```

你在页面输入 `/help` 后，会先看到已受理，再看到共享命令服务返回的结果；刷新页面记录仍在。QQ、Telegram、飞书继续使用相同处理器，没有复制第二套命令行为。

### 余额查询和短信关联

余额服务先读取设备、ICCID 和当前 VoWiFi 状态，再选择唯一发送链路。VoWiFi 活跃时 SMS 只走 VoWiFi，USSD 只走 USSI；否则才调用当前 QMI、MBIM 或 AT 后端。服务本身不改变卡策略和射频状态。

查询记录在发送前落库，同一 ICCID 的第二条 pending 会明确冲突。等待上限为五分钟；完整回复到达后按 ICCID、创建时间和规范化发送者匹配，保存原文，并尝试解析金额。真实回复无法解析时仍标记“已收到但未解析”，不会生成假余额。

### 运营商规则

内置表覆盖 16 个目标 MCC/MNC。资料无法支持唯一查询码的运营商保持 `unsupported`，页面显示官方替代方式和限制。自定义规则保存在数据库，优先于内置规则；Go `regexp` 提供 RE2 语义。

CTExcel 的 `BAL -> 888` 来自 2026-08-11 的历史项目观察。本轮没有重新实发，因此证据类型改为 `project_observation`，资费保持 `unknown`。历史观察与本轮范围见 `carrier-query-evidence.md`。

### API 和页面

批准的 `/api/command-center/*`、`/api/balances`、设备作用域 balance-queries 和 carrier-query-rules 均已实现，并与旧 `/api/commands/*`、`/api/balance/*` 兼容地址复用处理器。全部位于现有 Bearer 中间件之后，OpenAPI 同步声明。

桌面页面使用左侧设备/余额栏和右侧时间线；移动端把设备区置顶，命令输入区固定在对话底部并使用 `safe-area-inset-bottom`。点选普通设备命令会自动带入当前设备 ID，危险快捷动作仍需二次确认。

### 远端部署

最终生产代码来自 `f2afd2d`，证据提交 `4748f37` 没有改变二进制内容。部署文件为：

```text
/home/yibai/vohive/vohive-open/dist/vohive_vf2afd2d_linux_amd64
SHA256 4b33cfbe95e22b7026f89a8e861eee62b3bf72c8b7b933ec7a9a165ccc5fa481
```

本地与远端哈希一致。systemd 保持 active/running、`NRestarts=0`，回滚 drop-in 已保留，上传临时文件已删除。启动时两块硬件曾短暂进入 QMI/IMEI 恢复，随后都回到 lifecycle online，SIM、Access、Tunnel、IMS、SMS 全部 ready，蜂窝网络为关闭状态。延时复查没有新增 warning 或 error。

## 验证情况

已通过：

- `go test -timeout=60s ./cmd/... ./internal/... ./pkg/...`
- 关键 API、命令、余额、通知和运营商包 race 测试
- 受影响包 `go vet`
- 前端 typecheck、lint、22 项测试和生产构建
- 远端 12 个批准方法未鉴权 401、10 命令目录、`/help` accepted/result、SSE、16 规则、两设备健康检查
- 远端激活窗口 error 日志 0；排除 Gin 路由注册后，实际 SMS、余额、USSD/USSI、拨号日志 0

未验证：

- Chrome 控制运行时没有可连接实例，因此没有真实桌面、手机、横屏截图，也没有点击、焦点、console 和 network 检查。
- 本轮没有获得新的运营商实发授权，因此没有发送 CTExcel `BAL` 或 giffgaff `INFO`。自动化测试只证明生产路由和状态机，不证明运营商本次送达。

详细脱敏输出保存在 `remote-smoke-evidence-20260812.md`。

## 后续

当前只剩浏览器人工能力阻塞。Chrome 控制通道恢复后，需要在 1440×900、390×844 和移动横屏完成登录、设备选择、命令补全、危险确认、分页、清空、规则编辑、SSE 重连、截图及 console/network 检查；该复审不得发送余额短信、USSD 或拨号。
