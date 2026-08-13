# 实时 VoWiFi 电话 Mission 日志

## 初始化

- **状态**: IN PROGRESS（等待整批愿景审查）
- **任务源**: 用户提供的“实时 VoWiFi 软电话与 DTMF 页面”实施计划
- **恢复说明**: Mission 建立前已有部分后端与前端代码落盘；所有这些改动仍需按 CSV 逐项 review、验证与提交，不能仅凭存在文件标记完成。
- **工作区安全**: 开始时无 staged 变更；存在与本任务无关的未提交改动，提交必须使用显式路径隔离，禁止 stash/reset。
- **当前项**: REVIEW-01 — 审查实时电话目标与交付证据

## VOICE-01: 扩展语音运行时订阅与异步呼叫

- **状态**: DONE
- **做了什么**: 增加可取消的来电与生命周期事件广播，保留旧 handler/dispatcher，来电 notifier 异步去重；新增立即返回真实 Call-ID 的异步 INVITE、busy 与 capture finalized 事件。
- **关键决策**: 慢消费者使用独立有限缓冲队列，不在 SIP actor 上同步执行；旧同步模拟呼叫 API 保持不变。
- **验证**: 60 秒硬超时定向测试与 race 均通过；覆盖两种注册顺序、重复 Call-ID、阻塞 handler/dispatcher 和 INVITE 最终响应前返回。
- **提交**: `e6542a7b0094b0118665073664959d44ebb18c9b`
- **下一步**: PHONE-01 — 实现电话会话、租约与历史 API

## MEDIA-01: 桥接 WebRTC 与 IMS 实时媒体

- **状态**: DONE
- **做了什么**: 增加 Pion PCMU PeerConnection、共享 ICE UDP mux、本机 IMS RTP 桥、PCMU 直通、PCMA 双向转换、20ms 有限抖动缓冲和包统计。
- **根因修复**: 集成测试发现多个排队 RTP 包共享 UDP 循环读缓冲，乱序时会重复转码同一 payload；入队前现复制独立数据报。
- **验证**: 本机真实 PeerConnection + UDP 双向测试覆盖 PCMU、PCMA、乱序、单包丢失、序列/时间戳与统计；phone 和原 RTP relay 定向 race 通过。
- **提交**: `0891bb7fa3a1551e9809be239aceaf07626d2513`
- **下一步**: PHONE-01 — 回收会话、租约和 API 提交

## PHONE-01: 实现电话会话、租约与历史 API

- **状态**: DONE
- **做了什么**: 增加每设备单通话状态机、浏览器控制租约、呼入 486 busy、异步外呼事件回放、15 秒媒体恢复、终态分类与去重、SSE Last-Event-ID、通话历史以及完整认证 API。
- **通知边界**: 即时来电继续走既有 notifier；终态结果使用独立通知事件，慢通知渠道与慢 SSE 客户端都不会阻塞 SIP 状态机。
- **验证**: 60 秒硬超时定向测试与 race 通过；真实本机 Pion offer 通过认证 HTTP API 创建媒体，并验证错误租约 403、DTMF 和挂断生产调用链。
- **提交**: `90c3505f3922b46acabb68da398acda7cf353284`
- **下一步**: MEDIA-02 — 接入 AMR 与 AMR-WB 双向转码

## MEDIA-02: 接入 AMR 与 AMR-WB 双向转码

- **状态**: DONE
- **做了什么**: 增加原生 AMR-NB/opencore 与 AMR-WB/vo-amrwbenc 双向 codec 装载、RFC 4867 octet-aligned 单帧处理、mode-set 选择、PCMU/PCM 与 8/16 kHz 重采样、RTP 时间戳换算和能力感知 SDP。
- **ABI 核验**: 对照上游头文件确认 AMR-NB 与 AMR-WB 编码函数；发现并修正 `E_IF_encode` mode/dtx 参数应为 C `int`。
- **缺库行为**: 当前主机没有 opencore-amr/vo-amrwbenc 动态库，因此没有声称真实原生向量已运行；服务只广告成功装载的 codec，AMR-only 呼入会明确返回 488，不会静默降级或伪装成功。
- **验证**: 确定性 codec 边界向量与真实 Pion PeerConnection + UDP 双向 AMR/AMR-WB 媒体调度通过；60 秒硬超时定向测试和 race 通过。
- **提交**: `fa1679a50b69e85cb126626153e979ec249d52d2`
- **下一步**: RECORD-01 — 完成录音与 PCAP 生命周期

## RECORD-01: 完成录音与 PCAP 生命周期

- **状态**: DONE
- **根因修复**: 既有 relay 音频录制只写 IMS→浏览器单向，不能满足“双向混音”；现在由电话会话持有双向 PCMU→PCM 混音 WAV，并在媒体恢复时复用同一录音器。
- **PCAP 与发布**: relay 继续抓取双向 IMS RTP/RTCP；终态等待 capture finalize，再将混音 WAV 转为 MP3 并把 MP3/PCAP 文件名写入历史。
- **依赖修复**: WAV→MP3 只装载 LAME，不再因为 AMR 解码库缺失而阻断 G.711 混音录音发布。
- **失败语义**: 空录音、写入溢出、关闭、转码等错误写入 `recording_error` 并发出 `recording_failed`，不会把 completed 通话改成失败。
- **验证**: 双向混音样本、WAV header、MP3/PCAP 元数据、转码失败和认证 HTTP Range 206 均通过；定向 race 和第三方 PCAP race 通过。
- **提交**: `58add4496324f7c35a7b373f7f55f255ac13ce3e`
- **下一步**: HTTPS-01 — 提供 HTTPS、本地 CA 与媒体端口配置

## HTTPS-01: 提供 HTTPS、本地 CA 与媒体端口配置

- **状态**: DONE
- **做了什么**: HTTP 管理端口和 TLS 1.2+ HTTPS 端口并行启动、共同失败/关闭；自动生成持久本地 CA 与带主机名/IP SAN 的服务器证书，支持正式 cert/key 覆盖。
- **密钥边界**: TLS 目录强制 0700，CA/服务器私钥强制 0600；API 只提供 CA 公钥证书，正式证书模式返回无本地 CA。
- **失败语义**: HTTPS 端口占用会在启动阶段明确返回并关闭已打开的 HTTP listener；WebRTC UDP 端口仍由 phone 媒体构造阶段同步绑定并明确失败。
- **验证**: 实际 HTTP/HTTPS 请求、本地 CA 信任、TLS 版本、SAN、权限、CA 复用、正式证书覆盖、错配 CA、CA 下载和双 server 关闭均通过；定向 race 通过。
- **提交**: `ede9ccb9cf61128008cd10ba50a452ac2e8d2fb1`
- **下一步**: FRONTEND-01 — 实现全局浏览器电话状态与媒体恢复

## FRONTEND-01: 实现全局浏览器电话状态与媒体恢复

- **状态**: DONE
- **做了什么**: 增加全局 Pinia 电话状态、认证 SSE 长连接、PCMU WebRTC 媒体控制器、sessionStorage 标签页租约恢复、显式接管、静音和认证录音获取。
- **恢复语义**: 页面刷新后保留旧租约但不假装旧 PeerConnection 仍存活；用户点击恢复后用旧租约原子绑定新媒体，失败则关闭新媒体并恢复旧控制凭据。
- **所有权边界**: 共享 SSE 事件始终是只读视图，前端仅在事件 `media_id` 与本标签页媒体一致时显示可控；重复或倒序事件不会再次应用。
- **资源边界**: 只有受信任 HTTPS 安全上下文才申请单声道麦克风；PCMU/8000 缺失明确失败，协商失败、接听失败和结束均关闭 PeerConnection、音轨与远端 Audio 资源。
- **验证**: 44 项前端 test、typecheck、lint 全部通过；补充 SSE 分片/游标、媒体控制器、租约存储与 store 所有权测试；`internal/phone` 60 秒硬超时 race 通过。
- **提交**: `0ef1556389dd9c8736b81b59b73f7a5877e8d17b`
- **下一步**: FRONTEND-02 — 构建电话页和全局通话条

## FRONTEND-02: 构建电话页和全局通话条

- **状态**: BLOCKED（仅浏览器实测）
- **做了什么**: 增加 `/phone`、侧栏及移动导航入口、固定尺寸 3×4 拨号盘、就绪设备选择、呼入接听/拒接、DTMF、静音、挂断、恢复/接管、最近通话分类与认证 MP3 播放。
- **全局状态**: 受保护布局持有电话 store 和全局通话条，切换路由不会销毁 PeerConnection；通话条可静音、挂断并返回电话页。
- **安全上下文**: HTTP 页面明确说明实时语音不可用并提供 CA 下载和默认 HTTPS 入口；接听/外呼只在受信任 HTTPS 申请麦克风，拒接使用不申请麦克风的 receive-only 控制媒体。
- **可访问性**: 主操作具备文本或 aria-label，DTMF 和通话状态有 live region，移动导航及通话操作不小于 44px，状态同时提供文字而非只依赖颜色。
- **自动验证**: 50 项 test、typecheck、lint 和生产 build 全部通过；Vite 生产编译成功。
- **浏览器阻塞**: 已按 Browser 技能连接并读取排障文档，但浏览器运行时 `list()` 为空，无法执行桌面/移动真实视口、键盘焦点和可交互状态遍历；未以静态断言冒充浏览器结果。
- **提交**: `84eabe1ca30249304a66c9edcf0494bb09df7ae5`
- **下一步**: QA-01 — 补齐自动化回归与 API 文档

## QA-01: 补齐自动化回归与 API 文档

- **状态**: DONE
- **OpenAPI**: 实际嵌入的 `internal/api/openapi.vohive.yaml` 已覆盖 13 个电话路径、必选/可选 `X-Phone-Lease`、SSE 游标、CA 公钥下载、媒体/电话/历史 schema、DTMF 限制和 MP3 Range 响应。
- **配置示例**: 既有 HTTPS 提交已同步 `config/config.example.yaml` 的 7575/7576/7580、本地 TLS、正式证书覆盖和 STUN/TURN 示例，本行复核无漂移。
- **Go 验证**: 用真正控制整条命令时长的 60 秒包装器运行生产树 `cmd/... internal/... pkg/...` 全量测试通过；`internal/phone` 与 `internal/api` race 通过；第三方 `voicehost/voice/media` 的订阅、异步呼叫、呼入、DTMF、RTP 和 PCAP 定向 race 通过。
- **范围说明**: `go test ./...` 会把用户未跟踪的 `old/` 损坏快照当作根模块源码并因 `undefined1/void` 语法失败；未删除或修改该目录，生产全量改为显式生产树范围。
- **前端验证**: 最终 51 项 test、typecheck、lint、build 全部实际运行并通过。
- **审查修复**: 发现刷新恢复错误地跳过所有 `ringing`；现只跳过无 `media_id` 的未认领呼入，外呼响铃和已认领呼入均会重绑媒体。
- **提交**: `30b20d972b8c2c0a8702e345b488fbcc241ae48f`、`7c99a8857728a830ce472d108f962884903e3d7e`
- **下一步**: LIVE-01 — 验证 CTExcel 实机外呼与 IVR

## LIVE-01: 验证 CTExcel 实机外呼与 IVR

- **状态**: DONE（浏览器听觉交互受限）
- **部署与设备**: 用户授权后在 `192.168.11.179` 隔离构建并部署电话版本；`wwan1` 明确为 CTExcel，VoWiFi、IMS 和 voice ready，HTTPS `7576`、WebRTC UDP `7580` 均监听，自动 CA 对服务器 IP 校验通过。
- **首通发现与修复**: 第一通真实 `888` 已接通、按 `1` 并正常 BYE，但 MP3 因混音 WAV 缺少 `fmt ` chunk ID 失败；新增失败回归断言并修正 44 字节 WAV header，提交 `9bd74bb95160c14630ede51de8a3660bf0fa4fac` 后重部署。
- **第二通实机结果**: Call-ID `vohive-433f9a3dd56029147d9c0edb43861b76`，`calling → connected → completed`，AMR，14 秒，`local_hangup`；发送一次 DTMF `1`，随后 BYE，无活动电话残留。
- **媒体证据**: WebRTC 下行 584 包；PCAP 共 1304 包/14.12 秒，含 584 个下行 AMR、707 个上行 AMR和 13 个 PT101 RFC 4733 event-id=1 包，事件时长从 160 增至 1600，并以 3 个结束包收尾。按键前后 PCMU RMS 为 440/2544。
- **录音证据**: 历史 `recording_error` 为空；认证 HTTPS Range 返回 206。MP3 为 8kHz 单声道、32kbps、57600 字节、14.4 秒，SHA-256 `229b682bebe8eb6c149393d413ca6f61690589c1c2bdc18a99693683e56889ec`；PCAP 与 MP3 名均已持久化。
- **验证限制**: Browser MCP 已实际调用并按排障流程确认 runtime 列表为空，无法从真实页面点击、人工听取菜单语义或播放录音；没有把 RTP 能量变化写成人工听觉确认。

## LIVE-02: 验证授权实机来电通知一致性

- **状态**: BLOCKED
- **阻塞**: 新版本尚未部署、Browser 不可用，且没有可协调的授权外部来电源；不能擅自对真实号码发起来电。
- **替代证据边界**: 自动测试覆盖已接不报未接、远端取消 missed、用户 rejected、设备 busy，以及 notifier/页面订阅共存与慢渠道不阻塞；这些结果没有被表述为实机三方对账。

## DOCKER-01: 打包电话原生音频运行库与媒体端口

- **状态**: DONE
- **运行库**: Alpine 运行层安装 `opencore-amr`、`vo-amrwbenc` 和 `lame-libs`，构建时直接检查 AMR-NB、AMR-WB 和 MP3 的四个 `.so.0`。
- **端口**: 镜像声明管理 HTTP `7575/tcp`、电话 HTTPS `7576/tcp` 与 WebRTC mux `7580/udp`。
- **构建修复**: 生产构建不再执行会拉取无关测试依赖并修改模块清单的 `go mod tidy`，改为 `go build -mod=readonly`。
- **验证**: 两份 Dockerfile 的 `linux/amd64` 完整镜像均实际构建成功；容器内四个关键编码/解码符号、二进制可执行性和 ExposedPorts 均通过，最终镜像约 26.6 MB。
- **受限项**: Docker Hub 代理持续 EOF/TLS 超时，无法拉取 arm64 基础镜像，因此未声称 arm64 本地镜像构建通过；目标服务器 `x86_64` 已覆盖。
- **提交**: `ba3f4b2178f3567dbe7c2673632fd1017704f2ac`

## LISTEN-01: 实现无需麦克风的仅听接听模式

- **状态**: DONE
- **浏览器媒体**: 电话页新增“仅听接听”，用 `recvonly` WebRTC transceiver 建立下行音频，不调用 `getUserMedia`；HTTP 页面可使用该入口，麦克风接听和仅听后升级双向语音仍要求受信任 HTTPS。
- **服务端保活**: 服务端解析浏览器音频方向；仅听会话在 IMS media attach 后按 20ms 发送连续序列号和时间戳的静音 RTP。PCMU 直通，PCMA 转换，AMR/AMR-WB 经已协商原生 codec 编码，发送失败计入丢包统计。
- **状态与录音**: Pinia 明确保存 `listen-only`/`two-way` 模式，断线可恢复仅听，HTTPS 可用原租约替换媒体并升级；静音控件不会把仅听模式伪装成麦克风状态，静音帧也进入双向混音录音时间线。
- **界面边界**: HTTP 提示明确说明仍可仅听接听且“对方听不到你”；来电操作区区分拒接、仅听接听、麦克风接听，全局通话条同步显示仅听状态；交互目标、间距和焦点态按 UI 检查补齐。
- **验证**: `internal/phone` 完整测试与 race 均在 60 秒硬超时内通过；真实本机 Pion PeerConnection + UDP 覆盖 PCMU、PCMA、AMR、AMR-WB 下行、静音 RTP、录音与清理；前端 57 项 test、typecheck、lint、build 通过；`go mod verify` 与 `go mod tidy -diff` 通过。
- **验证限制**: Browser 连接成功后可用运行时列表仍为空，无法执行真实 HTTP 来电、375px/横屏、焦点顺序和仅听后升级交互巡检；未以静态断言冒充浏览器结果。
- **提交**: `9166fc4fba0518418bac1dc02ef54ba546b9523e`
- **下一步**: REVIEW-01 — 在既有实机与浏览器阻塞边界下执行整批愿景审查

## LISTEN-02: 封闭仅听隐私与媒体故障清理边界

- **状态**: DONE
- **隐私修复**: 媒体只在请求模式与现有模式一致时复用；从双向切换到仅听时，会在创建新媒体请求前停止旧麦克风音轨，并重新协商 `recvonly`，不会继续复用 `sendrecv`。
- **故障传播**: 20ms 静音 RTP 的编码或 UDP 写入失败会记录真实错误、只上报一次 `failed` 并退出发送循环；如果故障早于通话绑定，状态会暂存并在绑定后进入既有媒体恢复超时挂断链路。
- **确定性清理**: `MediaSession.Close` 先关闭停止信号并等待静音 worker 退出，再关闭实时 codec、PeerConnection 和 UDP socket，避免并发使用已释放资源。
- **验证**: 先运行回归测试确认原问题可复现；修复后前端 61 项 test、typecheck、lint、build 全部通过，`internal/phone` 完整测试与 race 均在 60 秒硬超时内通过。
- **提交**: `b3594ab75b2d34540d84f545b67e71770ab1cbf4`
- **剩余边界**: LIVE-01、LIVE-02 仍受目标部署、真实浏览器和授权来电源阻塞，本修复未把自动化结果描述为实机验证。

## LISTEN-03: 支持无需麦克风的仅听外呼

- **状态**: DONE（真实浏览器点击与听觉验收受限）
- **外呼模式**: HTTP 与 HTTPS 电话页都提供“仅听呼叫”，使用 `recvonly` WebRTC 且不申请麦克风；HTTPS 同时保留“双向呼叫”，不会在两种模式间静默切换。
- **升级路径**: 仅听外呼接通后沿用既有“启用麦克风”路径替换媒体，不重新拨号；HTTP 页面继续明确提示对方听不到用户。
- **错误清理**: 仅听媒体创建后若外呼 API 失败，会释放 PeerConnection、租约和模式状态，并把真实错误显示给用户。
- **验证**: 前端 72 项测试、typecheck、lint、production build 和 `go test -timeout=60s ./internal/phone` 通过；新增测试覆盖两种外呼媒体选择与失败清理。
- **浏览器限制**: Browser 控制运行时按排障流程检查后仍没有可用实例，因此未执行真实 HTTP/HTTPS 点击、麦克风权限和移动视口验收，也没有发起未授权电话。
- **提交**: `266f56be5a8d04be0666eb5e41a687c87aa44107`
