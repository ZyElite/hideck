# 浏览器存储审计

审计日期：2026-08-14。范围为生产代码中直接使用的 `localStorage` 与 `sessionStorage`，不包含测试、原型、旧版本、构建产物和第三方 Swagger UI 代码。

## 判定原则

- 只有需要跨浏览器、跨设备或服务重启保持，且能明确数据所有者和生命周期的数据，才适合进入服务端数据库。
- 浏览器标签页控制权、短期导航状态和本机界面偏好不应因为“能够持久化”就放进共享数据库。
- 凭证从 `localStorage` 移走时，目标应是安全 Cookie 与服务端会话，而不是把可重放的 Bearer Token 原文复制进数据库。

## 身份与会话类数据

| 存储键 | 位置与用途 | 当前生命周期 | 建议 | 结论 |
|---|---|---|---|---|
| `token` | `web/src/stores/auth.ts:10` 保存登录令牌；`web/src/services/phone.ts:118`、`web/src/composables/useEventStream.ts:47`、`web/src/components/DeviceEsimTab.vue:377` 和 `web/src/App.vue:126` 为原生流式请求读取；`internal/api/openapi.go:377` 供 API 文档读取 | 同一浏览器源下长期存在，退出时删除 | 改为 `Secure`、`HttpOnly`、`SameSite` Cookie。若需要主动注销、会话列表和逐设备吊销，再增加数据库 `web_sessions`，只保存不透明会话 ID 的哈希、所有者、过期时间与吊销时间 | **应迁移出 localStorage；数据库只承载服务端会话，不保存明文 Token** |
| `post_login_redirect` | `web/src/stores/auth.ts:61` 在 401 时记录当前路由，`web/src/views/Login.vue:33` 登录后读取并立即删除 | 当前标签页的一次登录跳转 | 保留 `sessionStorage`。它不属于业务数据，也不需要跨浏览器同步 | **不迁移数据库** |
| `hideck_phone_control` | `web/src/services/phone-session.ts:1` 保存当前标签页的 `mediaId` 与控制租约，用于刷新后恢复媒体控制 | 当前标签页；页面主动释放时删除 | 保留 `sessionStorage`。租约的安全裁决仍由服务端完成，把标签页控制权放进共享数据库会破坏“其他标签页只读”的边界 | **不迁移数据库** |
| `hideck-websheet-complete` | `internal/websheet/bridge.js:8` 写入带 Token 的完成回调，`web/src/components/CarrierWebsheetDialog.vue:97` 仅通过 `storage` 事件接收，作为 `postMessage`/`BroadcastChannel` 的兼容通道 | 浏览器源下会留下最后一次事件，但业务只把它当跨窗口通知 | 不进入数据库。后续可把它改成写入后立即清除的事件信号，避免完成回调和临时 Token 长期残留 | **不迁移数据库；建议缩短浏览器残留时间** |
| `ts43-go.odsa.callbacks`、`vowifi-go.vowifi.callbacks` | `internal/websheet/bridge.js:5`、`:6` 保存最近 20 条运营商页面回调，内容可能包含 activation code、ICCID、IMEI 和 URL | 浏览器源下长期保留；主要用于兼容页面内的 callback 查询接口 | 不应直接把原始数组搬进数据库。若产品明确需要跨浏览器审计历史，应由服务端按 websheet session 保存最小字段，设置短 TTL，并对 activation code 等敏感值脱敏或加密；否则改用会话内存或 `sessionStorage` | **有条件迁移：仅在需要服务端审计历史时** |

## 当前可迁移项

1. **认证会话（优先级高）**：从浏览器 Bearer Token 改为 HttpOnly Cookie；数据库会话表只在需要吊销和多设备会话管理时引入。
2. **运营商 Websheet 回调历史（需要产品决定）**：当前服务端 Broker 只在内存保留约 10 分钟（`internal/websheet/websheet.go:27`、`:57`）。只有确认需要跨浏览器历史或审计后，才增加带 TTL、最小化字段和敏感数据保护的数据库记录。

本节没有把任何会话数据直接迁入数据库；认证方式和运营商回调留存都涉及安全与产品语义，应作为独立变更实施和验证。

## 界面偏好与缓存类数据

| 存储键 | 位置与用途 | 当前生命周期 | 建议 | 结论 |
|---|---|---|---|---|
| `theme` | `web/index.html:11` 在 Vue 启动前应用主题，`web/src/App.vue:20`、`:37` 读取和切换主题 | 当前浏览器长期保留，避免刷新时明暗闪烁 | 当前是设备级界面偏好，保留浏览器。只有以后建立真实用户账户和“跨设备同步外观”设置时，才适合增加按用户保存的偏好；即使入库，仍应保留本地副本用于首屏主题 | **当前不迁移；未来可选同步** |
| `sidebar_collapsed` | `web/src/layouts/AuthenticatedShell.vue:38`、`:91` 记录桌面侧边栏是否折叠 | 当前浏览器长期保留，并受当前视口宽度影响 | 保留浏览器。不同屏幕宽度适合不同状态，跨设备同步反而容易产生错误布局 | **不迁移数据库** |
| `debug_panel_open` | `web/src/layouts/AuthenticatedShell.vue:98`、`:106`、`:129` 记录诊断抽屉开关 | 当前浏览器长期保留 | 它是临时工作区状态，数据库没有业务价值。若希望刷新后自动关闭，可另行改为 `sessionStorage` 或纯内存 | **不迁移数据库** |
| `debug_panel_auto_open` | `web/src/components/DebugPanel.vue:28`、`:34` 保存“发生错误时自动打开诊断面板”的开发偏好，`web/src/components/ErrorBoundary.vue:25` 使用 | 当前浏览器长期保留 | 保留浏览器。该行为与本机调试习惯有关，不应影响其他浏览器或其他管理员 | **不迁移数据库** |
| `hideck_show_sensitive` | `web/src/composables/useSensitiveVisibility.ts:4`、`:9`、`:18` 保存敏感字段是否可见 | 当前浏览器长期保留 | 不同步到数据库。敏感内容显示状态应由每个浏览器单独决定，并默认隐藏；服务端同步可能让另一台设备意外显示敏感值 | **不迁移数据库** |
| `go-4gproxy:mcc-mnc-table:v1` | `web/src/utils/mcc-mnc.ts:22`、`:110`、`:123` 缓存从第三方下载的完整 MCC/MNC 与运营商表，TTL 为 7 天；同时会合并 `/upstream-proxy-countries` 返回的服务端国家数据 | 当前浏览器独立缓存，可丢弃重建 | 应移出浏览器，但无需优先写入 SQLite。后端已经把同一来源缓存到 `data/mcc-mnc-table.json`（`internal/upstreamproxy/country_table.go:18`、`:78`、`:183`），更合理的方案是由后端 API 返回完整运营商行，让全部浏览器共用后端文件缓存和失败状态 | **应迁移到服务端权威缓存/API，不建议重复入数据库** |

## 完整结论

本次共识别 12 个生产存储键或键组，已经全部分类：

- **建议实施服务端迁移**：认证会话。浏览器改用 HttpOnly Cookie；是否增加数据库会话表取决于是否需要吊销和多设备管理。
- **建议移出浏览器但不进数据库**：MCC/MNC 表，直接复用后端现有文件缓存并通过 API 提供完整行。
- **需要产品决定后才能入库**：运营商 Websheet 回调历史。只有明确需要审计留存时才做带 TTL 和敏感字段保护的数据库模型。
- **继续留在浏览器**：登录回跳、电话控制租约、Websheet 跨窗口完成事件、主题、侧边栏、诊断面板和敏感字段显示状态。

免责声明接受状态不在上述清单中，因为它已经通过 `/api/settings/disclaimer` 和 `disclaimer_acceptances` 持久化；生产前端不存在 `disclaimer_agreed_at` 等浏览器接受标记。
