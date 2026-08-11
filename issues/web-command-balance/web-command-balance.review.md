## REVIEW-01

- Source doc: issues/web-command-balance/source.md
- Review agent: direct-spawn-agent
- Review independence: strong
- Review actual model: gpt-5
- Scope checked: 11 项命令中心、余额查询、运营商规则、API、Web 与部署承诺
- Evidence checked: cf65634..27ada09 提交、claim ledger、生产接线、Go/前端测试、本地构建和任务证据
- Claim coverage: gaps (11/11 checked)
- Claim/evidence alignment: mismatches found
- Limited validation honestly reported: partial
- Result: gaps_found
- Gaps: 批准 API 路径和方法未实现；短信回复规则允许空发送者；桌面/移动布局与设备参数选择不符；CTExcel 证据等级过高；远端原始证据未落盘；浏览器交互仍未验证
- Follow-up issues added: FOLLOWUP-01, FOLLOWUP-02, FOLLOWUP-03, REVIEW-02
- Assumptions: 保留现有 /api/commands 和 /api/balance 路由作为兼容别名，批准路径作为前端主路径
- Decision debt: 浏览器控制通道不可用，真实视觉验收必须等待 Chrome 扩展建立会话
- Human-required blockers: Chrome 浏览器控制通道当前无可连接实例

### Findings

1. `source.md` 指定 `/api/command-center/*`、设备作用域余额查询和 `/api/carrier-query-rules`，实现及 OpenAPI 使用 `/api/commands/*` 与 `/api/balance/*`，批准路径在远端返回 404。
2. `ResponseSMS` 规则可以保存空 `expected_senders`，而入站匹配只遍历该列表；这会在真实发送后必然超时。
3. 桌面端余额栏位于右侧，移动端余额栏位于对话区之后，输入区未处理安全区；普通设备命令快捷项仍需手写设备 ID。
4. CTExcel 被标为 `project_real_test`，当前受审提交没有持久化的可追溯送达证据，应补证据引用或降低声明等级。
5. PLAN-07 的真实远端结果只有 CSV 摘要，独立 reviewer 无法从仓库复核原始命令输出。
6. 浏览器、扩展和 native host 均存在，但浏览器控制运行时没有暴露实例；不得把类型检查和组件静态测试当作桌面/移动交互通过。

## REVIEW-02

- Source doc: issues/web-command-balance/source.md
- Review agent: direct-spawn-agent
- Review independence: strong
- Review actual model: gpt-5
- Scope checked: 11 项命令中心、余额查询、运营商规则、API、Web 与部署承诺，以及 REVIEW-01 的全部后续修复
- Evidence checked: 5d70098、1787306、f2afd2d、4748f37，claim ledger，Go/前端测试，运营商说明和脱敏远端证据
- Claim coverage: complete（11/11）
- Claim/evidence alignment: matched；10 项满足，1 项受限
- Limited validation honestly reported: yes
- Result: limited_review
- Gaps: 无可执行代码缺口；CLAIM-008 缺少真实浏览器三视口交互、截图、console 和 network 证据
- Follow-up issues added: REVIEW-03
- Assumptions: 4748f37 仅提交部署证据，未改变 f2afd2d 对应的远端生产代码
- Decision debt: 真实运营商余额查询仍须另行明确授权，不属于本轮无副作用验收
- Human-required blockers: Chrome 控制运行时没有可连接的浏览器实例，需要浏览器插件/会话恢复后复审

### 逐项结论

- CLAIM-001、002：十个命令由 Web 和通知渠道共享；执行、事件、分页、清理和 SSE 使用 SQLite 生产存储。
- CLAIM-003、004、005：余额查询严格二选一路由，不切射频、不重试；单 ICCID pending、五分钟、完整短信关联及原文/结构化结果均有集成证据。
- CLAIM-006、007：16 个预设完整；不确定规则明确受限或不支持；CTExcel 已降为 `project_observation`/`unknown`；内置只读和自定义 RE2 CRUD 均已接线。
- CLAIM-008：源码、结构测试和前端检查支持页面实现，但不能替代真实浏览器视觉与交互，状态为 limited。
- CLAIM-009：12 个批准方法/路径均在 Bearer 认证后并进入 OpenAPI，旧路由仍复用相同处理器。
- CLAIM-010、011：最终产物、远端服务/API/SSE/设备/日志证据已脱敏落盘；本轮没有真实短信、USSD/USSI 或拨号。

### 独立验证

- `go test -count=1 -timeout=60s ./internal/carrierquery ./internal/db ./internal/balance ./internal/commandcenter ./internal/notify ./internal/api`
- `go test -count=1 -timeout=60s ./internal/device -run 'Test(InboundSMSObserver|CellularSMSNotifiesObserver|VoWiFiSMSHistory|VoWiFi.*SMS|Pool.*SMS)'`
- 前端 typecheck、lint 和 22 项测试通过。
- 未连接远端、未重建 Linux 包、未执行浏览器或任何运营商副作用动作；远端部分审查仓库内已保存证据。
