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
