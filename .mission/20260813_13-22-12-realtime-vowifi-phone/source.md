# 实时 VoWiFi 软电话与 DTMF 页面

## 目标

新增独立 `/phone` 页面，支持选择 VoWiFi 设备、外呼、来电接听或拒接、浏览器双向实时语音、通话中 DTMF、静音、挂断、自动录音和最近通话。

## 核心约束

- 每台设备最多一通电话；创建或接听的浏览器独占控制，其他浏览器只读。
- 页面切换保持通话并显示全局通话条；媒体断线保留 15 秒，超时发送 BYE。
- 保留原有同步测试接口、自动任务、`SetNotifier` 和兼容的 `SetIncomingCallHandler`。
- 来电与终态事件按 Call-ID 去重，慢通知不得阻塞 SIP；终态区分 missed、rejected、busy、completed、failed。
- 浏览器使用单声道 PCMU/8000；IMS PCMU 直通、PCMA 转换，AMR/AMR-WB 双向逐帧转码，缺编码器时明确拒绝。
- 默认保存双向混音 MP3 与 IMS PCAP；录音失败不影响通话且必须如实记录。
- 提供计划列出的 `/api/phone/*` 接口、SSE Last-Event-ID 恢复、控制租约验证和 `voice_call_records`。
- 保留 HTTP `:7575`，新增 HTTPS `:7576`、WebRTC UDP mux `:7580`，支持自动本地 CA 和用户证书覆盖。
- 麦克风只在浏览器认可的 HTTPS 安全上下文启用，本地 CA 私钥不得通过 API 暴露。

## 验收

- 覆盖订阅并存与去重、异步外呼、呼入接听、DTMF、CANCEL/BYE、租约越权、15 秒恢复、关闭清理和终态分类。
- 覆盖 PCMU、PCMA、AMR、AMR-WB 双向媒体、RTP 连续性、DTMF 并发以及 MP3/PCAP。
- 前端运行 test、typecheck、lint、build；Go 测试硬超时 60 秒，并对会话、媒体和事件模块运行 race。
- 实机 CTExcel `wwan1` 外呼免费 `888`，听提示、发送 DTMF、确认 IVR、正常 BYE 并播放 MP3。
- 使用授权来电验证页面、原通知渠道和最近通话均收到正确且不重复的状态。
