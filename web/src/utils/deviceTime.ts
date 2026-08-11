export type DeviceTimeInfo = {
  now: string
  timezone: string
  offset_seconds: number
  source: string
}

export type DeviceTimeFormatOptions = {
  clientClock?: boolean
  fallback?: string
}

type DeviceClock = {
  timezone: string
  offsetSeconds: number
  clientDeltaMs: number
}

type DateParts = {
  year: string
  month: string
  day: string
  hour: string
  minute: string
  second: string
}

const naiveDateTimePattern = /^(\d{4})[-/](\d{2})[-/](\d{2})[ T](\d{2}):(\d{2}):(\d{2})(?:\.\d+)?$/
let deviceClock: DeviceClock | null = null
let datePartsFormatter: Intl.DateTimeFormat | null = null

export function configureDeviceTime(info: DeviceTimeInfo, requestStartedAt: number, responseReceivedAt: number) {
  const serverEpoch = Date.parse(info.now)
  if (!Number.isFinite(serverEpoch)) throw new Error('设备时间 API 返回了无效 now')
  if (!Number.isInteger(info.offset_seconds) || Math.abs(info.offset_seconds) > 24 * 60 * 60) {
    throw new Error('设备时间 API 返回了无效 offset_seconds')
  }
  const timezone = String(info.timezone || '').trim()
  if (timezone) validateTimezone(timezone)
  const source = String(info.source || '').trim()
  if (!source) throw new Error('设备时间 API 未返回检测来源')

  const midpoint = requestStartedAt + (responseReceivedAt - requestStartedAt) / 2
  deviceClock = {
    timezone,
    offsetSeconds: info.offset_seconds,
    clientDeltaMs: serverEpoch - midpoint
  }
  datePartsFormatter = null
}

export function resetDeviceTime() {
  deviceClock = null
  datePartsFormatter = null
}

export function deviceNow(clientNow = Date.now()) {
  if (!deviceClock) throw new Error('设备时间尚未同步')
  return clientNow + deviceClock.clientDeltaMs
}

export function formatDeviceDateTime(value: string | number | Date, options: DeviceTimeFormatOptions = {}) {
  const naive = parseNaiveDateTime(value)
  if (naive) return `${naive.year}-${naive.month}-${naive.day} ${naive.hour}:${naive.minute}:${naive.second}`
  const parts = resolveDateParts(value, options)
  if (!parts) return invalidTimeText(value, options)
  return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}:${parts.second}`
}

export function formatDeviceDate(value: string | number | Date, options: DeviceTimeFormatOptions = {}) {
  const naive = parseNaiveDateTime(value)
  if (naive) return `${naive.year}-${naive.month}-${naive.day}`
  const parts = resolveDateParts(value, options)
  if (!parts) return invalidTimeText(value, options)
  return `${parts.year}-${parts.month}-${parts.day}`
}

export function formatDeviceTime(value: string | number | Date, options: DeviceTimeFormatOptions = {}) {
  const naive = parseNaiveDateTime(value)
  if (naive) return `${naive.hour}:${naive.minute}:${naive.second}`
  const parts = resolveDateParts(value, options)
  if (!parts) return invalidTimeText(value, options)
  return `${parts.hour}:${parts.minute}:${parts.second}`
}

export function formatDeviceMonthDay(value: string | number | Date, options: DeviceTimeFormatOptions = {}) {
  const naive = parseNaiveDateTime(value)
  if (naive) return `${naive.month}-${naive.day}`
  const parts = resolveDateParts(value, options)
  if (!parts) return invalidTimeText(value, options)
  return `${parts.month}-${parts.day}`
}

function validateTimezone(timezone: string) {
  try {
    new Intl.DateTimeFormat('en-CA', { timeZone: timezone }).format(0)
  } catch {
    throw new Error(`设备时间 API 返回了无效 IANA 时区: ${timezone}`)
  }
}

function parseNaiveDateTime(value: string | number | Date): DateParts | null {
  if (typeof value !== 'string') return null
  const match = value.trim().match(naiveDateTimePattern)
  if (!match) return null
  return {
    year: match[1], month: match[2], day: match[3],
    hour: match[4], minute: match[5], second: match[6]
  }
}

function resolveDateParts(value: string | number | Date, options: DeviceTimeFormatOptions): DateParts | null {
  if (!deviceClock) return null
  let epoch = value instanceof Date ? value.getTime() : typeof value === 'number' ? value : Date.parse(value)
  if (!Number.isFinite(epoch)) return null
  if (options.clientClock && (typeof value === 'number' || value instanceof Date)) {
    epoch += deviceClock.clientDeltaMs
  }
  if (!deviceClock.timezone) epoch += deviceClock.offsetSeconds * 1000
  const formatter = currentDatePartsFormatter()
  const fields = Object.fromEntries(formatter.formatToParts(epoch).map(part => [part.type, part.value]))
  return {
    year: fields.year, month: fields.month, day: fields.day,
    hour: fields.hour, minute: fields.minute, second: fields.second
  }
}

function currentDatePartsFormatter() {
  if (datePartsFormatter) return datePartsFormatter
  const timeZone = deviceClock?.timezone || 'UTC'
  datePartsFormatter = new Intl.DateTimeFormat('en-CA', {
    timeZone, hourCycle: 'h23', year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit'
  })
  return datePartsFormatter
}

function invalidTimeText(value: string | number | Date, options: DeviceTimeFormatOptions) {
  if (!deviceClock) return '设备时间未同步'
  return options.fallback ?? (typeof value === 'string' ? value : '')
}
