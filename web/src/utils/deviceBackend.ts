export function isWwanQmiControlPath(path: string | null | undefined): boolean {
  const value = String(path || '').trim()
  if (!value) return false
  const basename = value.replace(/\\/g, '/').split('/').filter(Boolean).pop() || value
  return /^wwan\d+qmi\d+$/.test(basename)
}

export type ManagedDeviceBackend = 'qmi' | 'mbim'

export function normalizeManagedDeviceBackend(value: unknown): ManagedDeviceBackend | null {
  const backend = String(value || '').trim().toLowerCase()
  return backend === 'qmi' || backend === 'mbim' ? backend : null
}

export function isManagedDeviceBackendSwitch(previous: unknown, next: unknown): boolean {
  const from = normalizeManagedDeviceBackend(previous)
  const to = normalizeManagedDeviceBackend(next)
  return from !== null && to !== null && from !== to
}
