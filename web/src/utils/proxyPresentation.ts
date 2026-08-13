import type {
  ProxyDevice,
  ProxyInstance,
  ProxyInstanceStatus,
  UpstreamProxy
} from '../types/api'
import type { UpstreamProxyHealth } from './upstreamProxyHealth'

export type ProxyPresentationTone = 'danger' | 'neutral' | 'success' | 'warning'

export type UpstreamProxyPresentation = Readonly<{
  id: string
  name: string
  address: string
  enabled: boolean
  enabledLabel: string
  enabledTone: ProxyPresentationTone
  healthLabel: string
  healthTone: ProxyPresentationTone
  healthDetail: string
  authenticationLabel: string
  ruleCount: number
}>

export type OutboundProxyPresentation = Readonly<{
  id: string
  name: string
  endpoint: string
  enabled: boolean
  enabledLabel: string
  enabledTone: ProxyPresentationTone
  running: boolean
  runningLabel: string
  runningTone: ProxyPresentationTone
  modeLabel: string
  authenticationLabel: string
  deviceLabel: string
  lastError: string
}>

type UpstreamPresentationInput = Readonly<{
  proxy: UpstreamProxy
  ruleCount: number
  health?: UpstreamProxyHealth
}>

type OutboundPresentationInput = Readonly<{
  instance: ProxyInstance
  status?: ProxyInstanceStatus
  device?: ProxyDevice
}>

export function createUpstreamProxyPresentation({
  proxy,
  ruleCount,
  health
}: UpstreamPresentationInput): UpstreamProxyPresentation {
  const healthPresentation = presentUpstreamHealth(proxy.enabled, health)
  return Object.freeze({
    id: proxy.id,
    name: normalizedText(proxy.name) || normalizedText(proxy.id) || '未命名代理',
    address: normalizedText(proxy.addr) || '不可用',
    enabled: proxy.enabled,
    enabledLabel: proxy.enabled ? '已启用' : '已禁用',
    enabledTone: proxy.enabled ? 'success' : 'neutral',
    healthLabel: healthPresentation.label,
    healthTone: healthPresentation.tone,
    healthDetail: healthPresentation.detail,
    authenticationLabel: normalizedText(proxy.username) ? '账号认证' : '免认证',
    ruleCount: Math.max(0, Math.trunc(ruleCount))
  })
}

function presentUpstreamHealth(
  enabled: boolean,
  health: UpstreamProxyHealth | undefined
): Readonly<{ label: string; tone: ProxyPresentationTone; detail: string }> {
  if (!enabled || health?.state === 'disabled') {
    return { label: '未启用', tone: 'neutral', detail: health?.detail || '代理未启用' }
  }
  if (!health || health.state === 'checking') {
    return { label: '检测中', tone: 'warning', detail: health?.detail || '正在检测 UDP Associate' }
  }
  if (health.state === 'unhealthy') {
    return { label: '探测失败', tone: 'danger', detail: health.detail }
  }

  const duration = health.durationMs == null ? '' : ` · ${health.durationMs} ms`
  return { label: `UDP 可用${duration}`, tone: 'success', detail: health.detail }
}

export function createOutboundProxyPresentation({
  instance,
  status,
  device
}: OutboundPresentationInput): OutboundProxyPresentation {
  const running = status?.running === true
  const lastError = normalizedText(status?.last_error)

  return Object.freeze({
    id: instance.id,
    name: normalizedText(instance.name) || normalizedText(instance.id) || '未命名实例',
    endpoint: formatEndpoint(instance.listen_addr, instance.listen_port),
    enabled: instance.enabled,
    enabledLabel: instance.enabled ? '已启用' : '已禁用',
    enabledTone: instance.enabled ? 'success' : 'neutral',
    running,
    runningLabel: lastError ? '运行异常' : running ? '运行中' : '已停止',
    runningTone: lastError ? 'danger' : running ? 'success' : 'neutral',
    modeLabel: instance.mode === 'http' ? 'HTTP' : 'SOCKS5',
    authenticationLabel: instance.auth_enabled ? '账号认证' : '免认证',
    deviceLabel: formatDevice(instance.device_id, device),
    lastError
  })
}

function formatEndpoint(address: string | undefined, port: number | undefined): string {
  const normalizedAddress = normalizedText(address)
  if (!normalizedAddress || !Number.isInteger(port) || Number(port) <= 0) return '不可用'
  return `${normalizedAddress}:${port}`
}

function formatDevice(deviceId: string | undefined, device: ProxyDevice | undefined): string {
  if (device) {
    const name = normalizedText(device.name) || normalizedText(device.id)
    const deviceInterface = normalizedText(device.interface)
    return [name, deviceInterface].filter(Boolean).join(' / ') || '不可用'
  }
  return normalizedText(deviceId) || '未绑定'
}

function normalizedText(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}
