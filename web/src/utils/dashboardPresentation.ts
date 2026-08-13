import type { DashboardDevice, VoWiFiRuntimeState } from '../types/api'

export const DASHBOARD_UNAVAILABLE = '不可用'
export const DASHBOARD_UNASSIGNED = '未分配'

export type DashboardConnectionStage = Readonly<{
  key: 'SIM' | 'Access' | 'Tunnel' | 'IMS' | 'SMS'
  ready: boolean | undefined
}>

export type DashboardDevicePresentation = Readonly<{
  connectionState: string
  connectionTitle: string
  connectionType: string
  displayName: string
  ipv4: string
  ipv6: string
  operator: string
  signal: string
  stages: readonly DashboardConnectionStage[]
  statusLabel: '在线' | '离线'
}>

const SIGNAL_SENTINELS = new Set([0, -999])

export function hasDashboardSignal(value: unknown): value is number {
  return typeof value === 'number'
    && Number.isFinite(value)
    && !SIGNAL_SENTINELS.has(value)
}

export function formatDashboardNetworkType(device: DashboardDevice): string {
  if (device.vowifi_active) return 'VoWiFi'
  const parts = [device.network_duplex, device.network_mode]
    .map((value) => String(value || '').trim())
    .filter(Boolean)
  return parts.join(' ') || DASHBOARD_UNAVAILABLE
}

export function formatDashboardSignal(value: unknown): string {
  return hasDashboardSignal(value) ? `${value} dBm` : DASHBOARD_UNAVAILABLE
}

export function createDashboardStages(
  runtime?: VoWiFiRuntimeState
): readonly DashboardConnectionStage[] {
  return Object.freeze([
    Object.freeze({ key: 'SIM', ready: runtime?.sim_ready }),
    Object.freeze({ key: 'Access', ready: runtime?.access_ready }),
    Object.freeze({ key: 'Tunnel', ready: runtime?.tunnel_ready }),
    Object.freeze({ key: 'IMS', ready: runtime?.ims_ready }),
    Object.freeze({ key: 'SMS', ready: runtime?.sms_ready })
  ])
}

export function createDashboardDevicePresentation(
  device: DashboardDevice
): DashboardDevicePresentation {
  const connectionType = formatDashboardNetworkType(device)
  const isOnline = device.healthy
  const isVoWiFi = isOnline && device.vowifi_active === true

  return Object.freeze({
    connectionState: getConnectionState(isOnline, isVoWiFi, connectionType),
    connectionTitle: getConnectionTitle(device, isOnline, isVoWiFi),
    connectionType,
    displayName: String(device.name || device.id).trim() || device.id,
    ipv4: normalizeAddress(device.public_ip),
    ipv6: normalizeAddress(device.public_ipv6),
    operator: normalizeFact(device.operator),
    signal: formatDashboardSignal(device.signal_dbm),
    stages: createDashboardStages(device.vowifi_runtime),
    statusLabel: isOnline ? '在线' : '离线'
  })
}

function getConnectionState(
  isOnline: boolean,
  isVoWiFi: boolean,
  connectionType: string
): string {
  if (!isOnline) return '当前设备不可用'
  if (isVoWiFi) return '已连接'
  return connectionType === DASHBOARD_UNAVAILABLE ? '控制面在线' : connectionType
}

function getConnectionTitle(
  device: DashboardDevice,
  isOnline: boolean,
  isVoWiFi: boolean
): string {
  if (!isOnline) return '设备离线'
  if (isVoWiFi) return 'Wi-Fi Calling'
  return normalizeFact(device.operator, '网络检测中')
}

function normalizeAddress(value: unknown): string {
  return String(value || '').trim() || DASHBOARD_UNASSIGNED
}

function normalizeFact(value: unknown, fallback = DASHBOARD_UNAVAILABLE): string {
  return String(value || '').trim() || fallback
}
