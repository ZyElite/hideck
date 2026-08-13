import { computed, reactive, ref } from 'vue'
import type { DeviceMgmtListItem } from '../types/api'
import type { ServiceResult } from '../types/domain'
import type { BalanceQuery } from '../types/commands'

type DeviceListPayload = Readonly<{ devices: DeviceMgmtListItem[] }>

type RuntimeStatusSources = Readonly<{
  fetchDevices: () => Promise<ServiceResult<DeviceListPayload>>
  fetchBalances: () => Promise<ServiceResult<BalanceQuery[]>>
  onForegroundError?: (message: string) => void
  now?: () => number
  refreshIntervalMs?: number
}>

type RuntimeRefreshOptions = Readonly<{ background?: boolean }>

const DEFAULT_REFRESH_INTERVAL_MS = 5_000

export function useCommandRuntimeStatus(sources: RuntimeStatusSources) {
  const devices = ref<DeviceMgmtListItem[]>([])
  const balances = ref<BalanceQuery[]>([])
  const lastSyncedAt = ref<number | null>(null)
  const refreshing = ref(false)
  const errors = reactive({ devices: '', balances: '' })
  const syncWarning = computed(() => [
    errors.devices ? `设备状态：${errors.devices}` : '',
    errors.balances ? `余额状态：${errors.balances}` : ''
  ].filter(Boolean).join('；'))

  let activeRefresh: Promise<boolean> | null = null
  let refreshTimer: number | null = null
  let disposed = false

  function refresh(options: RuntimeRefreshOptions = {}) {
    if (activeRefresh) return activeRefresh
    const background = options.background === true
    if (!background) refreshing.value = true
    activeRefresh = performRefresh(background).finally(() => {
      activeRefresh = null
      if (!background) refreshing.value = false
    })
    return activeRefresh
  }

  async function performRefresh(background: boolean) {
    const [deviceResult, balanceResult] = await Promise.all([
      sources.fetchDevices(),
      sources.fetchBalances()
    ])
    if (disposed) return false

    applyDeviceResult(deviceResult)
    applyBalanceResult(balanceResult)
    const succeeded = deviceResult.ok && balanceResult.ok
    if (succeeded) lastSyncedAt.value = (sources.now || Date.now)()
    if (!succeeded && !background) sources.onForegroundError?.(syncWarning.value)
    return succeeded
  }

  function applyDeviceResult(result: ServiceResult<DeviceListPayload>) {
    if (!result.ok) {
      errors.devices = result.error.message || '设备列表加载失败'
      return
    }
    devices.value = result.data.devices
    errors.devices = ''
  }

  function applyBalanceResult(result: ServiceResult<BalanceQuery[]>) {
    if (!result.ok) {
      errors.balances = result.error.message || '余额记录加载失败'
      return
    }
    balances.value = result.data
    errors.balances = ''
  }

  function startPolling() {
    if (refreshTimer !== null || disposed) return
    const interval = sources.refreshIntervalMs || DEFAULT_REFRESH_INTERVAL_MS
    refreshTimer = window.setInterval(() => void refresh({ background: true }), interval)
  }

  function dispose() {
    disposed = true
    if (refreshTimer === null) return
    window.clearInterval(refreshTimer)
    refreshTimer = null
  }

  return {
    devices,
    balances,
    lastSyncedAt,
    refreshing,
    syncWarning,
    refresh,
    startPolling,
    dispose
  }
}
