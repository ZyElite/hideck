export function routeDeviceStillManaged(routeDeviceId: string, currentId: string, knownIds: string[]): boolean {
  const id = String(routeDeviceId || '').trim()
  if (!id || currentId === id) return false
  if (knownIds.length > 0 && !knownIds.includes(id)) return false
  return true
}

export function firstRemainingDeviceId(knownIds: string[], currentId: string): string {
  const current = String(currentId || '').trim()
  if (current && knownIds.includes(current)) return current
  return knownIds[0] || ''
}

export function suggestedAddDeviceId(device: {
  net_interface?: string
  reader_name?: string
} | null | undefined): string {
  const iface = String(device?.net_interface || '').trim()
  if (iface) return iface
  const reader = String(device?.reader_name || '').trim()
  if (!reader) return ''
  return reader.replace(/[^\w.-]+/g, '_').replace(/^_+|_+$/g, '').slice(0, 48)
}

export function isPCSCServiceUnavailable(message: string): boolean {
  const text = String(message || '').toLowerCase()
  return text.includes('service is unavailable') ||
    text.includes('0x8010001d') ||
    text.includes('scard_e_no_service')
}
