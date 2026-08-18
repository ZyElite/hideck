# 运营商实测与预设备注

记录 2026-08-18 会话结论，避免和「能上网」「手机能收短信」混在一起。

## EC25 和短信 / 电话

- 这块现场模组是 `2c7c:0125` Quectel EC25（大疆改身份一类）。QMI WMS、NAS、VOICE 能分配；原生 IMS/IMSA/IMSP 分配失败（`qmi service not supported by hardware`）。
- **上网**：EC25 本职，QMI 拨号即可。别人改模组上网走的是这条，不是 VoLTE。
- **国内卡驻自家网**：短信可以走原版驻网（关软件电话、射频开、QMI/AT）。国内 VoLTE / 公网 VoWiFi 打电话，这块 EC25 做不了可靠方案。代码里 MCC `460`/`461` 会直接拒绝 VoWiFi。
- **手机能收、棒子驻网收不到**：认证手机有 VoLTE/IMS 短信；EC25 关掉软件 IMS 后只剩 QMI。英国卡漫游中国移动时，刚才实测 WMS 发送超时、入站没有 URC。

## giffgaff（234/10）

- 有完整预设 `giffgaff_23410`。WiFi calling（关射频 + 软件 IMS）能 REGISTER 200、`contact_smsip=true`。
- 关射频时发 `INFO`→`85075`：O2 IP-SM-GW 回 **RP-ERROR 69 facility not implemented**。短号 IMS 被网元拒，不是没注册。
- 切成「蜂窝 + on_demand」会空闲拆隧道（`VOWIFI_DESIRED_RECOVER_SKIPPED_CELLULAR_IDLE`），此时既没有 IMS 也没有可靠的 QMI 入站。
- 原版驻网收短信：关 VoWiFi → 射频 Online → QMI 听/发，没有射频抑制、没有蜂窝空闲。原版没有另一套短信协议。
- 2026-08-18 在国内把 giffgaff 收成干净驻网（关 IMS、驻中国移动、不开流量）后：自发 `INFO`/自发短信 QMI 超时；手机发来的短信没有进模组。当时状态不是原版日常「从未开过 IMS 的驻网」。
- CTEUK 不要给 giffgaff 发短信（收费）。CTEUK 只测免费查询（`BAL`→`888`）和打电话。giffgaff 可以自己发 `INFO`。

## 英国沃达丰 / VOXI（234/15）

- VOXI 是 Vodafone Limited 的品牌，和沃达丰 UK 同一张网，IMSI 一般是 `23415…`。
- 原版 vohive / 当时的预设清单里 **没有** `vodafone_uk`，只有荷兰 `vodafone_nl_20404`。
- 现已加入预设 `vodafone_uk_23415`：标准 ePDG `epdg.epc.mnc015.mcc234.pub.3gppnetwork.org`，`device_model=rmx3366`，IKE/ESP 先用英国已通提案加宽列表，REGISTER 带 `smsip`。
- 国家代理仍按 MCC `234` 走 GB 规则。VOXI 官方写明漫游不支持 WiFi calling，人在国内需走代理。
- 真卡验收前：IKE/ESP 仍可能要按日志改。余额查询未做自动短码（走 App）。

## 英国侧已有预设

| 运营商 | PLMN | 预设 |
| --- | --- | --- |
| giffgaff | 234/10 | `giffgaff_23410` |
| Vodafone UK / VOXI | 234/15 | `vodafone_uk_23415` |
| Three UK | 234/20 | `three_uk_234020` |
| CTExcel | 234/33 | `CTEUK_23433` |
