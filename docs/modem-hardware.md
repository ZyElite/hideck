# 模组硬件与协议说明

本文整理 HiDeck 使用高通模组时涉及的实体 SIM 检测、USB 身份恢复，以及 AT、QMI、MBIM 通道说明。

## EC25 与 SIM 检测

EC25 的 USB/QMI 热插拔和实体 SIM 检测是两个独立功能。大疆定制模块实测应关闭实体 SIM 热插拔检测：

```text
AT+QSIMDET=0,0
AT+CFUN=1,1
```

`QSIMDET=0,0` 会关闭基于 `SIM_DET` 引脚的检测。请在模组断电时插好 SIM 后重新上电，也可以在插卡后执行 `AT+CFUN=1,1` 软重启。

重启后通过 AT 端口验证：

```text
AT+QSIMDET?
AT+CPIN?
AT+QCCID
```

预期 `AT+QSIMDET?` 返回 `+QSIMDET: 0,0`，`AT+CPIN?` 返回 `+CPIN: READY`，并且 `AT+QCCID` 能读取 ICCID。参数说明参见 [Quectel EC25 & EC21 AT Commands Manual](https://quectel.com/content/uploads/2021/03/Quectel_EC25EC21_AT_Commands_Manual_V1.3.pdf)。

### 大疆定制模块恢复为移远 USB 身份

以下步骤仅适用于 USB 当前识别为 `2ca3:4006`、且已经确认底层为移远 EC25 的大疆定制模块。该操作会持久化 USB VID/PID 和接口组合；错误参数可能导致 AT 口或网络接口消失，不要用于其他型号。

```bash
sudo apt-get update && sudo apt-get install socat -y
sudo modprobe option

echo 2ca3 4006 | sudo tee /sys/bus/usb-serial/drivers/option1/new_id
echo 'AT+QCFG="usbcfg",0x2C7C,0x0125,1,1,1,1,1,0,0' | socat - /dev/ttyUSB2,crnl
echo 'AT+CFUN=1,1' | socat - /dev/ttyUSB2,crnl
```

等待重新枚举后运行 `lsusb`，预期包含：

```text
2c7c:0125 Quectel Wireless Solutions Co., Ltd. EC25 LTE modem
```

如果 `/dev/ttyUSB2` 不存在或不是 AT 口，必须先确认实际端口，不能直接发送持久化配置。当前用户也必须拥有串口读写权限。参数说明参见 [Quectel EC2x/EG2x/EG9x/EM05 QCFG AT Commands Manual](https://quectel.com/content/uploads/2024/02/Quectel_EC2xEG2xEG9xEM05_Series_QCFG_AT_Commands_Manual_V1.0.pdf)。

## AT、QMI 与 MBIM

| 通道 | Linux 常见节点/驱动 | 用途 |
| --- | --- | --- |
| AT | `/dev/ttyUSB*`、`/dev/ttyACM*` | 模组配置、诊断、短信和人工命令 |
| QMI | `/dev/cdc-wdm*` + `qmi_wwan` | 高通模组控制面、SIM、短信和蜂窝数据拨号 |
| MBIM | `/dev/cdc-wdm*` + `cdc_mbim` | USB-IF 标准化控制面和蜂窝数据拨号 |

同一块模组可以保留 AT 串口，同时把网络控制组合配置为 QMI 或 MBIM。HiDeck 在 QMI 模式管理控制面和数据面；在 MBIM 模式管理网络，短信和人工 AT 命令仍由同一个 AT 调度器串行执行。

MBIM 支持取决于具体硬件和固件的 USB 组合，不能只看产品系列或 `AT+QCFG="usbnet"` 是否接受参数。切换后至少确认：

1. 网卡由 `cdc_mbim` 驱动并出现 `/dev/cdc-wdm*`；
2. 标准 MBIM `OPEN` 能收到 `OPEN_DONE`；
3. `DeviceCaps` 能返回当前模组 IMEI。

只出现 `cdc_mbim` 接口不足以证明协议可用。当前测试的大疆定制 EC25 在 `usbnet=2` 时可以枚举 `cdc_mbim`，但标准 `OPEN` 无响应，因此 HiDeck 会拒绝持久化 MBIM 并保留或恢复 QMI 配置。其他型号必须逐台验证。
