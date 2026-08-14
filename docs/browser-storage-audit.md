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
