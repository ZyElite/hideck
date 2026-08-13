import type { DeviceMgmtListItem } from '../types/api'

export type SmsDeviceChannelPresentation = Readonly<{
  id: string
  label: string
  statusLabel: string
  detail: string
  online: boolean
  operator?: string
  accessibilityLabel: string
}>

export type SmsConversationThreadContext = Readonly<{
  deviceId?: string
  lastDeviceName?: string
}>

export type SmsConversationContext = Readonly<{
  deviceLabel: string
  operatorLabel: string
  smsLabel: string
  smsTone: 'success' | 'danger' | 'muted'
  imsLabel: string
  imsTone: 'success' | 'danger' | 'muted'
}>

export function createSmsDeviceChannels(
  devices: readonly DeviceMgmtListItem[]
): readonly SmsDeviceChannelPresentation[] {
  const rows = devices.map(createDeviceChannel)
  const onlineCount = rows.filter(row => row.online).length
  const allStatus = `${onlineCount}/${rows.length} 在线`
  return Object.freeze([
    Object.freeze({
      id: 'all',
      label: '全部设备',
      statusLabel: allStatus,
      detail: `${rows.length} 台设备`,
      online: onlineCount > 0,
      accessibilityLabel: `全部设备，${allStatus}`
    }),
    ...rows
  ])
}

export function createSmsConversationContext(options: Readonly<{
  selectedDeviceId: string
  thread: SmsConversationThreadContext | null
  devices: readonly DeviceMgmtListItem[]
}>): SmsConversationContext {
  const device = resolveConversationDevice(options)
  return Object.freeze({
    deviceLabel: device?.name || options.thread?.lastDeviceName || options.thread?.deviceId || '全部设备',
    operatorLabel: cleanValue(device?.modem?.operator) || '运营商未提供',
    ...readinessContext(device)
  })
}

export function smsUnreadBadge(unreadCount: number, readLocally = false): number {
  if (readLocally) return 0
  return Math.max(0, Number(unreadCount) || 0)
}

export function smsPeerInitial(peer: string): string {
  return Array.from(cleanValue(peer))[0]?.toLocaleUpperCase() || '—'
}

function createDeviceChannel(device: DeviceMgmtListItem): SmsDeviceChannelPresentation {
  const online = device.running && (device.control_online ?? device.healthy) === true
  const statusLabel = online ? '在线' : '离线'
  const operator = cleanValue(device.modem?.operator)
  const label = cleanValue(device.name) || device.id
  return Object.freeze({
    id: device.id,
    label,
    statusLabel,
    detail: operator ? `${statusLabel} · ${operator}` : statusLabel,
    online,
    operator,
    accessibilityLabel: `${label}，${statusLabel}${operator ? `，${operator}` : ''}`
  })
}

function resolveConversationDevice(options: Readonly<{
  selectedDeviceId: string
  thread: SmsConversationThreadContext | null
  devices: readonly DeviceMgmtListItem[]
}>): DeviceMgmtListItem | undefined {
  const targetId = options.selectedDeviceId === 'all'
    ? options.thread?.deviceId
    : options.selectedDeviceId
  if (!targetId) return undefined
  return options.devices.find(device => device.id === targetId)
}

function readinessContext(device: DeviceMgmtListItem | undefined) {
  const runtime = device?.vowifi_runtime
  return {
    smsLabel: readinessLabel(runtime?.sms_ready, 'SMS', runtime?.sms_ready_reason),
    smsTone: readinessTone(runtime?.sms_ready),
    imsLabel: readinessLabel(runtime?.ims_ready, 'IMS'),
    imsTone: readinessTone(runtime?.ims_ready)
  } as const
}

function readinessLabel(value: boolean | undefined, subject: string, reason?: string): string {
  if (value === true) return subject === 'SMS' ? 'VoWiFi · SMS 已就绪' : 'IMS 已注册'
  if (value === false) return cleanValue(reason) || `${subject} 未就绪`
  return `${subject} 状态未提供`
}

function readinessTone(value: boolean | undefined): 'success' | 'danger' | 'muted' {
  if (value === true) return 'success'
  if (value === false) return 'danger'
  return 'muted'
}

function cleanValue(value: string | undefined): string {
  return String(value || '').trim()
}
