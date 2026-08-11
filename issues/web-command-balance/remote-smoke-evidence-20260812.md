# 命令中心最终远端冒烟证据

验收时间：2026-08-12 01:17 至 01:21 CST  
目标：`yibai@192.168.11.179:/home/yibai/vohive/vohive-open`

本文件只保留复核所需的脱敏摘要。鉴权 token 在远端进程内临时生成，没有输出或写盘；输出不包含密码、token、ICCID、IMSI 或 IMEI。

## 产物与服务

```text
artifact=dist/vohive_vf2afd2d_linux_amd64
artifact_sha256=4b33cfbe95e22b7026f89a8e861eee62b3bf72c8b7b933ec7a9a165ccc5fa481
local_remote_sha256_match=true
ActiveState=active
SubState=running
MainPID=2265766
NRestarts=0
ExecStart=/home/yibai/vohive/vohive-open/dist/vohive_vf2afd2d_linux_amd64 -c /home/yibai/vohive/vohive-open/config/config.yaml
rollback_backup=present
upload_temp=removed
delayed_recheck_same_pid=true
delayed_recheck_nrestarts=0
warnings_after_convergence=0
errors_since_activation=0
```

服务切换后第一次 `/ping` 在监听建立前返回连接拒绝，下一次成功；部署脚本在成功探测后才结束。最终 `/ping` 和 `/commands` 均为 HTTP 200。

## 鉴权边界

以下批准路由在无 Bearer token 时均返回 HTTP 401：

```text
GET    /api/command-center/commands
POST   /api/command-center/executions
GET    /api/command-center/events
GET    /api/command-center/stream
DELETE /api/command-center/history
GET    /api/balances
POST   /api/devices/wwan0/balance-queries
GET    /api/devices/wwan0/balance-queries
GET    /api/carrier-query-rules
POST   /api/carrier-query-rules
PUT    /api/carrier-query-rules/custom
DELETE /api/carrier-query-rules/custom
```

兼容读取路由保持可用：

```text
compat_endpoint=/api/commands/catalog http=200
compat_endpoint=/api/balance/queries http=200
compat_endpoint=/api/balance/rules http=200
```

## 命令与事件

```text
catalog_count=10
catalog_names=balance,esim,help,list,rotate,send,sms,status,switch,vocall
help_http=202
help_events=accepted,result
help_state=completed
sse_bytes=4206
sse_id_lines=6
sse_command_events=6
```

`/help` 是本次唯一执行的命令，不会操作设备或运营商链路。SSE 使用两秒客户端窗口读取持久化事件后按预期由客户端超时结束。

## 规则与设备

```text
builtin_rules=16
cte_evidence=project_observation
cte_cost=unknown
health_http=200
health_state=healthy
device_count=2
health_device=wwan0 healthy=true network_connected=false
health_device=wwan1 healthy=true network_connected=false
device=wwan0 running=true control=true lifecycle=online vowifi=true sim=true access=true tunnel=true ims=true sms=true network=false
device=wwan1 running=true control=true lifecycle=online vowifi=true sim=true access=true tunnel=true ims=true sms=true network=false
balance_list_device=wwan0 http=200
balance_list_device=wwan1 http=200
```

启动窗口中 `wwan1` 曾出现 QMI 初始化超时，`wwan0` 曾短暂未按 IMEI 枚举；服务未重启，两块设备随后分别完成重新绑定和 VoWiFi 目标态恢复，以上是收敛后的状态。

## 日志与副作用

```text
error_log_lines=0
actual_sms_ussd_call_log_lines=0
```

初筛曾匹配到三行 `/actions/ussd`，逐行核对后确认都是 Gin 启动时的路由注册输出。排除路由注册后，本验收窗口没有实际 SMS、余额查询、USSD/USSI 或拨号记录。本轮没有执行真实 CTExcel `BAL` 或 giffgaff `INFO` 查询。
