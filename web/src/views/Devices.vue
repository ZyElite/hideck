<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import ErrorState from '../components/ErrorState.vue'
import RefreshButton from '../components/RefreshButton.vue'
import DeviceListPanel from '../components/DeviceListPanel.vue'
import DeviceDetailHeader from '../components/DeviceDetailHeader.vue'
import DeviceDetailLoading from '../components/DeviceDetailLoading.vue'
import DeviceOverviewTab from '../components/DeviceOverviewTab.vue'
import DeviceEsimTab from '../components/DeviceEsimTab.vue'
import DeviceAtTab from '../components/DeviceAtTab.vue'
import DeviceUssdTab from '../components/DeviceUssdTab.vue'
import DeviceConfigTab from '../components/DeviceConfigTab.vue'
import CardPolicyPanel from '../components/CardPolicyPanel.vue'
import DeviceAddDialog from '../components/DeviceAddDialog.vue'
import CarrierWebsheetDialog from '../components/CarrierWebsheetDialog.vue'
import TrafficAnalysisPanel from '../components/TrafficAnalysisPanel.vue'
import { usePollingScheduler } from '../composables/usePollingScheduler'
import { useEventStream } from '../composables/useEventStream'
import { useDevicesStore } from '../stores/devices'
import { debugCollector } from '../debug/collector'
import { isManagedDeviceBackendSwitch, isWwanQmiControlPath } from '../utils/deviceBackend'
import { isControlOnline, isRecoveryPhase } from '../utils/deviceLifecycle'
import { createDeviceRequestScope } from '../utils/deviceRequestScope'
import { getMccMncIndex, lookupMccMncRow, mccMncCountryCode, type MccMncRow } from '../utils/mcc-mnc'
import type { CardPolicy, CarrierWebsheetInfo, DeviceConfigDTO, DeviceMgmtListItem, DeviceOverviewItem, DiscoveredDevice, ModemStatus, PNNRecord, RealtimeTrafficSnapshot } from '../types/api'
import type { AppError } from '../types/domain'
import { toAppError } from '../services/http'
import { devicesService } from '../services/devices'
import { cardsService } from '../services/cards'
import { createEmptyTrafficAnalysis, trafficService, type TrafficRange } from '../services/traffic'
import {
  ArrowSync24Regular,
  Add24Regular,
  Sim24Regular
} from '@vicons/fluent'

const router = useRouter()
const route = useRoute()
const devicesStore = useDevicesStore()
const { list: storeList, discovered: storeDiscovered, pcscDiscoveryError, deviceLimit } = storeToRefs(devicesStore)

let listAbort: AbortController | null = null
let detailAbort: AbortController | null = null
let trafficAbort: AbortController | null = null

const detailPollFailCount = ref(0)
const listPollFailCount = ref(0)
const detailPollWarned = ref(false)
const listPollWarned = ref(false)

const loading = ref(true)
const activeTab = ref('overview')
const loadLastOkAt = ref<number | null>(null)
const loadError = ref<{ message: string; status?: number; method?: string; url?: string } | null>(null)
const devices = ref<DeviceMgmtListItem[]>([])
const discovered = ref<DiscoveredDevice[]>([])
const query = ref('')
const statusFilter = ref<'all' | 'online' | 'offline'>('all')
const sortKey = ref<'name' | 'signal'>('name')
const sortDir = ref<'asc' | 'desc'>('asc')
const selectedId = ref('')
const selectedDetail = ref<DeviceOverviewItem | null>(null)
const detailLoading = ref(false)
const detailError = ref<AppError | null>(null)
const hasAutoSelected = ref(false)
const deviceTabs = new Set(['overview', 'esim', 'at', 'ussd', 'config', 'card'])

const editConfig = ref<DeviceConfigDTO | null>(null)
const editConfigDeviceId = ref('')
const editBaseline = ref('')
const editDirty = ref(false)
const configLoading = ref(false)
const configError = ref<AppError | null>(null)
const saving = ref(false)
const rotating = ref(false)
const reconnectingVoWiFi = ref(false)
const e911Starting = ref(false)
const e911WebsheetOpen = ref(false)
const e911Websheet = ref<CarrierWebsheetInfo | null>(null)
const deleting = ref(false)
const rescanning = ref(false)

// 卡策略（跟当前选中设备的 ICCID 绑定）
const cardPolicy = ref<CardPolicy | null>(null)
const cardPolicyLoading = ref(false)
const cardPolicyError = ref<AppError | null>(null)
const configRequestScope = createDeviceRequestScope('')
const cardPolicyRequestScope = createDeviceRequestScope('')

type LoadOutcome =
  | Readonly<{ status: 'ok' }>
  | Readonly<{ status: 'failed'; error: AppError }>
  | Readonly<{ status: 'stale' }>

const trafficSpeedRx = ref('')
const trafficSpeedTx = ref('')
const rollingMinuteRx = ref('')
const rollingMinuteTx = ref('')
const realtimeTrafficActiveUntil = ref(0)
const deviceAnalysis = ref(createEmptyTrafficAnalysis())
const deviceAnalysisLoading = ref(false)
const deviceAnalysisLastOkAt = ref<number | null>(null)
const deviceAnalysisError = ref<AppError | null>(null)
const deviceAnalysisRange = ref<TrafficRange>('day')

const addableDiscovered = computed(() => discovered.value.filter((item) => !item.configured))

const addDialogOpen = ref(false)
const addSelected = ref<DiscoveredDevice | null>(null)
const addSaving = ref(false)
const addConfig = ref<DeviceConfigDTO>({
  id: '',
  name: '',
  interface: '',
  modem_imei: '',
  usb_path: '',
  esim_transport: 'at',
  at_port: '',
  control_device: '',
  device_backend: 'at'
})

const discovering = ref(false)

type RollingTrafficSample = {
  at: number
  rxBytes: number
  txBytes: number
}

const realtimeTrafficWindowMs = 60_000
let rollingTrafficWindow: RollingTrafficSample[] = []

const filteredDevices = computed<DeviceMgmtListItem[]>(() => {
  const q = String(query.value || '').trim().toLowerCase()
  let list = devices.value.slice()

  if (statusFilter.value === 'online') {
    list = list.filter(d => isControlOnline(d))
  } else if (statusFilter.value === 'offline') {
    list = list.filter(d => !d?.running && !isRecoveryPhase(d.lifecycle_phase))
  }

  if (q) {
    list = list.filter(d => {
      const hay = `${d?.id || ''} ${d?.name || ''} ${d?.modem?.iccid || ''} ${d?.modem?.imei || ''} ${d?.interface || ''}`.toLowerCase()
      return hay.includes(q)
    })
  }

  list.sort((a, b) => {
    let av = 0
    let bv = 0
    if (sortKey.value === 'name') {
      const an = (a.name || a.id || '').toLowerCase()
      const bn = (b.name || b.id || '').toLowerCase()
      if (an < bn) return sortDir.value === 'asc' ? -1 : 1
      if (an > bn) return sortDir.value === 'asc' ? 1 : -1
      return 0
    }
    if (sortKey.value === 'signal') {
      av = Number(a?.modem?.signal_dbm ?? -999)
      bv = Number(b?.modem?.signal_dbm ?? -999)
      return sortDir.value === 'asc' ? av - bv : bv - av
    }
    return 0
  })

  return list
})

const selectedListItem = computed<DeviceMgmtListItem | null>(() => {
  return devices.value.find(d => d.id === selectedId.value) || null
})

const selectedDevice = computed<DeviceOverviewItem | null>(() => {
  return selectedDetail.value
})

const RADIO_LIVE_GRACE_MS = 3000

type LiveRadioFields = Pick<
  ModemStatus,
  'operator' | 'signal_dbm' | 'signal_rsrp' | 'signal_rsrq' | 'signal_sinr' | 'nr5g_signal_sinr' | 'radio_band' | 'radio_channel' | 'reg_status' | 'reg_status_text' | 'network_mode' | 'network_duplex' | 'operating_mode'
>

type LiveRadioCacheEntry = {
  capturedAt: number
  radio: LiveRadioFields
}

const liveRadioCache = reactive(new Map<string, LiveRadioCacheEntry>())
const pendingRawSelectedDetail = ref<DeviceOverviewItem | null>(null)
const mccMncIndex = ref<Map<string, MccMncRow> | null>(null)
let liveRadioFallbackTimer: number | null = null

function normalizeSPN(v: unknown): string {
  return String(v ?? '').trim()
}

function firstQueryValue(value: unknown): string {
  if (Array.isArray(value)) return String(value[0] ?? '')
  return String(value ?? '')
}

function applyRouteSelection(): boolean {
  const tab = firstQueryValue(route.query.tab).trim()
  if (deviceTabs.has(tab)) {
    activeTab.value = tab
  }

  const deviceID = firstQueryValue(route.query.device).trim()
  if (!deviceID || selectedId.value === deviceID) {
    return false
  }

  selectedId.value = deviceID
  hasAutoSelected.value = true
  return true
}

function nativeMccMnc(modem: ModemStatus | undefined): string {
  const mcc = String(modem?.native_mcc ?? '').trim()
  const mnc = String(modem?.native_mnc ?? '').trim()
  return mcc && mnc ? `${mcc}${mnc}` : ''
}

function pnnDisplayName(record: PNNRecord | undefined): string {
  return normalizeSPN(record?.full_name) || normalizeSPN(record?.short_name)
}

function firstPNNName(records: PNNRecord[] | undefined): string {
  if (!Array.isArray(records)) return ''
  for (const r of records) {
    const name = pnnDisplayName(r)
    if (name) return name
  }
  return ''
}

function oplMatchesNativePLMN(oplPLMN: string | undefined, nativePLMN: string): boolean {
  const pattern = String(oplPLMN ?? '').trim().toLowerCase()
  if (!pattern || !nativePLMN) return false
  if (pattern === nativePLMN) return true
  if (!pattern.includes('x')) return pattern.length < nativePLMN.length && nativePLMN.startsWith(pattern)
  if (pattern.length !== nativePLMN.length) return false
  for (let i = 0; i < pattern.length; i++) {
    if (pattern[i] !== 'x' && pattern[i] !== nativePLMN[i]) return false
  }
  return true
}

function pnnNameFromOPL(modem: ModemStatus | undefined): string {
  const nativePLMN = nativeMccMnc(modem)
  if (!nativePLMN || !Array.isArray(modem?.opl) || !Array.isArray(modem?.pnn)) return ''
  for (const opl of modem.opl) {
    if (!oplMatchesNativePLMN(opl?.plmn, nativePLMN)) continue
    const pnnRecord = Number(opl?.pnn_record ?? 0)
    if (!pnnRecord) continue
    const name = pnnDisplayName(modem.pnn.find((record) => record.record === pnnRecord))
    if (name) return name
  }
  return ''
}

function formatNamedOperator(name: string, code: string): string {
  if (!code) return name
  return `${name} (${code})`
}

function formatMccMncOperator(code: string): string {
  const index = mccMncIndex.value
  if (!index || !code) return code
  const row = lookupMccMncRow(index, code)
  if (!row) return code
  const name = normalizeSPN(row.network) || normalizeSPN(row.country)
  return name ? formatNamedOperator(name, code) : code
}

function extractLiveRadioFields(detail: DeviceOverviewItem): LiveRadioFields {
  return {
    operator: detail.modem?.operator,
    signal_dbm: detail.modem?.signal_dbm,
    signal_rsrp: detail.modem?.signal_rsrp,
    signal_rsrq: detail.modem?.signal_rsrq,
    signal_sinr: detail.modem?.signal_sinr,
    nr5g_signal_sinr: detail.modem?.nr5g_signal_sinr,
    radio_band: detail.modem?.radio_band,
    radio_channel: detail.modem?.radio_channel,
    reg_status: detail.modem?.reg_status,
    reg_status_text: detail.modem?.reg_status_text,
    network_mode: detail.modem?.network_mode,
    network_duplex: detail.modem?.network_duplex,
    operating_mode: detail.modem?.operating_mode
  }
}

function mergeLiveRadioFields(detail: DeviceOverviewItem, radio: LiveRadioFields): DeviceOverviewItem {
  return {
    ...detail,
    modem: {
      ...detail.modem,
      ...radio
    }
  }
}

function clearLiveRadioFallbackTimer() {
  if (liveRadioFallbackTimer !== null) {
    window.clearTimeout(liveRadioFallbackTimer)
    liveRadioFallbackTimer = null
  }
  pendingRawSelectedDetail.value = null
}

function scheduleLiveRadioFallback(detail: DeviceOverviewItem, remainingMs: number) {
  clearLiveRadioFallbackTimer()
  pendingRawSelectedDetail.value = detail
  if (remainingMs <= 0) {
    selectedDetail.value = detail
    pendingRawSelectedDetail.value = null
    updateTrafficSpeedFromSelected()
    return
  }
  const expectedID = detail.id
  liveRadioFallbackTimer = window.setTimeout(() => {
    liveRadioFallbackTimer = null
    const pending = pendingRawSelectedDetail.value
    pendingRawSelectedDetail.value = null
    if (!pending || selectedId.value !== expectedID || pending.id !== expectedID) return
    selectedDetail.value = pending
    updateTrafficSpeedFromSelected()
  }, remainingMs)
}

function resolveDetailForDisplay(detail: DeviceOverviewItem | null): DeviceOverviewItem | null {
  if (!detail) {
    clearLiveRadioFallbackTimer()
    return null
  }

  if (detail.radio_live_ok === true) {
    liveRadioCache.set(detail.id, {
      capturedAt: Date.now(),
      radio: extractLiveRadioFields(detail)
    })
    clearLiveRadioFallbackTimer()
    return detail
  }

  if (detail.radio_live_ok !== false) {
    clearLiveRadioFallbackTimer()
    return detail
  }

  const cached = liveRadioCache.get(detail.id)
  if (!cached) {
    clearLiveRadioFallbackTimer()
    return detail
  }

  const age = Date.now() - cached.capturedAt
  if (age >= RADIO_LIVE_GRACE_MS) {
    clearLiveRadioFallbackTimer()
    return detail
  }

  scheduleLiveRadioFallback(detail, RADIO_LIVE_GRACE_MS - age)
  return mergeLiveRadioFields(detail, cached.radio)
}

const selectedSimOperatorDisplay = computed(() => {
  const d = selectedDevice.value
  if (!d) return '--'
  const spn = normalizeSPN(d?.modem?.native_spn)
  const pnn = pnnNameFromOPL(d?.modem) || firstPNNName(d?.modem?.pnn)
  const mccmnc = nativeMccMnc(d?.modem)
  if (spn) return formatNamedOperator(spn, mccmnc)
  if (pnn) return formatNamedOperator(pnn, mccmnc)
  return mccmnc ? formatMccMncOperator(mccmnc) : '--'
})

const selectedSimOperatorCountryCode = computed(() => {
  const code = nativeMccMnc(selectedDevice.value?.modem)
  return mccMncCountryCode(mccMncIndex.value, code)
})

function formatBytesPerSecond(bps: unknown) {
  const v = Number(bps) || 0
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s']
  let val = v
  let i = 0
  while (val >= 1024 && i < units.length - 1) {
    val /= 1024
    i++
  }
  return `${val.toFixed(i === 0 ? 0 : 1)}${units[i]}`
}

function formatBytes(bytes: unknown) {
  const v = Number(bytes) || 0
  const units = ['B', 'KB', 'MB', 'GB']
  let val = v
  let i = 0
  while (val >= 1024 && i < units.length - 1) {
    val /= 1024
    i++
  }
  return `${val.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

function resetRollingTrafficWindow() {
  rollingTrafficWindow = []
  rollingMinuteRx.value = ''
  rollingMinuteTx.value = ''
}

function setRollingTrafficWindowStatus(value: string) {
  rollingTrafficWindow = []
  rollingMinuteRx.value = value
  rollingMinuteTx.value = value
}

function updateRollingTrafficWindow(rxDeltaBytes: unknown, txDeltaBytes: unknown, at = Date.now()) {
  const cutoff = at - realtimeTrafficWindowMs
  rollingTrafficWindow = rollingTrafficWindow.filter(sample => sample.at >= cutoff)
  rollingTrafficWindow.push({
    at,
    rxBytes: Math.max(0, Number(rxDeltaBytes) || 0),
    txBytes: Math.max(0, Number(txDeltaBytes) || 0)
  })

  let rxBytes = 0
  let txBytes = 0
  for (const sample of rollingTrafficWindow) {
    rxBytes += sample.rxBytes
    txBytes += sample.txBytes
  }
  rollingMinuteRx.value = formatBytes(rxBytes)
  rollingMinuteTx.value = formatBytes(txBytes)
}

function updateTrafficSpeedFromSelected() {
  const d = selectedDetail.value
  if (!d || !d.network_connected || !d.traffic_raw || d.traffic_meta?.status !== 'ok') {
    trafficSpeedRx.value = ''
    trafficSpeedTx.value = ''
    resetRollingTrafficWindow()
    return
  }
  if (Date.now() < realtimeTrafficActiveUntil.value) {
    return
  }
  const rx = Number(d.traffic_raw?.bytes_received ?? 0)
  const tx = Number(d.traffic_raw?.bytes_sent ?? 0)
  trafficSpeedRx.value = formatBytesPerSecond(Math.max(0, rx) / 60)
  trafficSpeedTx.value = formatBytesPerSecond(Math.max(0, tx) / 60)
  resetRollingTrafficWindow()
}

function handleRealtimeTrafficEvent(data: RealtimeTrafficSnapshot) {
  if (!data || data.device_id !== selectedId.value) return

  realtimeTrafficActiveUntil.value = Date.now() + 2500
  if (data.status === 'ok') {
    trafficSpeedRx.value = formatBytesPerSecond(Math.max(0, Number(data.rx_bps) || 0))
    trafficSpeedTx.value = formatBytesPerSecond(Math.max(0, Number(data.tx_bps) || 0))
    updateRollingTrafficWindow(data.rx_delta_bytes, data.tx_delta_bytes)
    return
  }
  if (data.status === 'waiting_sample') {
    trafficSpeedRx.value = '等待采样'
    trafficSpeedTx.value = '等待采样'
    setRollingTrafficWindowStatus('等待采样')
    return
  }
  if (data.status === 'reset') {
    trafficSpeedRx.value = formatBytesPerSecond(0)
    trafficSpeedTx.value = formatBytesPerSecond(0)
    setRollingTrafficWindowStatus(formatBytes(0))
    return
  }
  trafficSpeedRx.value = '采样中断'
  trafficSpeedTx.value = '采样中断'
  setRollingTrafficWindowStatus('采样中断')
}

function resetDeviceTrafficAnalysis() {
  if (trafficAbort) {
    trafficAbort.abort()
    trafficAbort = null
  }
  deviceAnalysis.value = createEmptyTrafficAnalysis()
  deviceAnalysisLoading.value = false
  deviceAnalysisError.value = null
  deviceAnalysisLastOkAt.value = null
}

async function fetchDeviceTrafficAnalysis(id = selectedDetail.value?.id || selectedId.value) {
  const detail = selectedDetail.value
  const deviceID = String(id || '').trim()
  if (!deviceID) {
    resetDeviceTrafficAnalysis()
    return
  }
  if (detail?.id === deviceID && !detail.network_connected) {
    resetDeviceTrafficAnalysis()
    return
  }

  if (trafficAbort) trafficAbort.abort()
  const controller = new AbortController()
  trafficAbort = controller
  deviceAnalysisLoading.value = true
  deviceAnalysisError.value = null

  const result = await trafficService.getAnalysis(deviceAnalysisRange.value, deviceID, controller.signal)
  if (trafficAbort !== controller) return

  if (!result.ok) {
    if (result.error.code !== 'ERR_CANCELED') {
      deviceAnalysis.value = createEmptyTrafficAnalysis()
      deviceAnalysisError.value = result.error
    }
    trafficAbort = null
    deviceAnalysisLoading.value = false
    return
  }

  deviceAnalysis.value = result.data
  deviceAnalysisLastOkAt.value = Date.now()
  trafficAbort = null
  deviceAnalysisLoading.value = false
}

function clearSelectedDetail() {
  clearLiveRadioFallbackTimer()
  selectedDetail.value = null
  updateTrafficSpeedFromSelected()
}

function emptyResponseError(resource: string, url: string): AppError {
  return {
    message: `${resource}响应为空`,
    method: 'GET',
    url
  }
}

async function fetchSelectedDetail(id: string): Promise<LoadOutcome> {
  const deviceID = String(id || '').trim()
  if (!deviceID) {
    clearSelectedDetail()
    detailError.value = null
    detailLoading.value = false
    return { status: 'ok' }
  }

  if (selectedDetail.value?.id !== deviceID) clearSelectedDetail()
  detailError.value = null
  detailLoading.value = true
  if (detailAbort) detailAbort.abort()
  const controller = new AbortController()
  detailAbort = controller
  const result = await devicesStore.fetchDetail(deviceID, controller.signal)
  if (detailAbort !== controller || selectedId.value !== deviceID) {
    return { status: 'stale' }
  }
  detailAbort = null
  detailLoading.value = false

  if (!result.ok) {
    if (result.error.code === 'ERR_CANCELED') return { status: 'stale' }
    clearSelectedDetail()
    detailError.value = result.error
    return { status: 'failed', error: result.error }
  }

  const detail = result.data as DeviceOverviewItem | null
  if (!detail) {
    const error = emptyResponseError('设备详情', `/devices/${deviceID}/overview`)
    clearSelectedDetail()
    detailError.value = error
    return { status: 'failed', error }
  }

  selectedDetail.value = resolveDetailForDisplay(detail)
  updateTrafficSpeedFromSelected()
  return { status: 'ok' }
}

async function syncEditConfigFromSelected(force = false) {
  const id = String(selectedId.value || '').trim()
  if (!force && editDirty.value && editConfigDeviceId.value === id) return
  const requestToken = configRequestScope.begin(id)
  editConfig.value = null
  editConfigDeviceId.value = id
  editBaseline.value = ''
  editDirty.value = false
  configError.value = null
  configLoading.value = Boolean(id)
  if (!id) {
    return
  }

  try {
    const result = await devicesStore.fetchConfig(id)
    if (!configRequestScope.isCurrent(requestToken, selectedId.value)) return
    if (!result.ok) {
      configError.value = result.error
      return
    }
    if (!result.data) {
      configError.value = emptyResponseError('设备配置', `/devices/${id}/config`)
      return
    }
    editConfig.value = JSON.parse(JSON.stringify(result.data)) as DeviceConfigDTO

    if (!editConfig.value.device_backend) {
      editConfig.value.device_backend = 'at'
    }
    editBaseline.value = JSON.stringify(editConfig.value)
    editDirty.value = false
  } catch (error: unknown) {
    if (configRequestScope.isCurrent(requestToken, selectedId.value)) {
      configError.value = toAppError(error)
    }
  } finally {
    if (configRequestScope.isCurrent(requestToken, selectedId.value)) {
      configLoading.value = false
    }
  }
}

watch(
  editConfig,
  (v) => {
    if (!v) {
      editDirty.value = false
      editBaseline.value = ''
      return
    }
    const cur = JSON.stringify(v)
    if (!editBaseline.value) {
      editBaseline.value = cur
      editDirty.value = false
      return
    }
    editDirty.value = cur !== editBaseline.value
  },
  { deep: true }
)

async function fetchCardPolicy(iccid: string | undefined) {
  const cardID = String(iccid || '').trim()
  const requestToken = cardPolicyRequestScope.begin(cardID)
  cardPolicy.value = null
  cardPolicyError.value = null
  cardPolicyLoading.value = Boolean(cardID)
  if (!cardID) return

  try {
    const result = await cardsService.getPolicy(cardID)
    const currentCardID = String(selectedDetail.value?.modem?.iccid || '').trim()
    if (!cardPolicyRequestScope.isCurrent(requestToken, currentCardID)) return
    if (!result.ok) {
      cardPolicyError.value = result.error
      return
    }
    cardPolicy.value = result.data
  } catch (error: unknown) {
    const currentCardID = String(selectedDetail.value?.modem?.iccid || '').trim()
    if (cardPolicyRequestScope.isCurrent(requestToken, currentCardID)) {
      cardPolicyError.value = toAppError(error)
    }
  } finally {
    const currentCardID = String(selectedDetail.value?.modem?.iccid || '').trim()
    if (cardPolicyRequestScope.isCurrent(requestToken, currentCardID)) {
      cardPolicyLoading.value = false
    }
  }
}

// 卡策略热切换后：刷新卡策略 + 概览详情（让概览即时反映网络/VoWiFi/飞行模式面板切换）
async function onCardPolicyChanged() {
  await Promise.all([
    fetchCardPolicy(selectedDetail.value?.modem?.iccid),
    refreshSelectedDetailOnly()
  ])
}

watch(
  () => selectedDetail.value?.modem?.iccid,
  (iccid) => { void fetchCardPolicy(iccid) },
  { immediate: true }
)

async function fetchAll() {
  loading.value = true
  loadError.value = null
  try {
    const prevSelected = selectedId.value
    if (listAbort) listAbort.abort()
    listAbort = new AbortController()
    const listResult = await devicesStore.fetchList(listAbort.signal)
    if (!listResult.ok) throw new Error(listResult.error.message)
    devices.value = (storeList.value || []) as DeviceMgmtListItem[]
    loadLastOkAt.value = Date.now()
    applyRouteSelection()

    if (!hasAutoSelected.value && !selectedId.value && devices.value.length) {
      selectedId.value = devices.value[0].id
      hasAutoSelected.value = true
    }
    // 移除强行重置 selectedId 的逻辑，因为这会在轮询期间当设备短暂拿不到时，把界面强制拉回到第一项。
    const selectedStillExists = selectedId.value
      ? devices.value.some(d => d.id === selectedId.value)
      : false
    if (selectedId.value && !selectedStillExists && devices.value.length === 0) {
      selectedId.value = ''
    }
    
    // 如果之前没有选中的id或者这次选中改变了，则加载详情
    const selectionChanged = prevSelected !== selectedId.value || (selectedDetail.value?.id || '') !== selectedId.value
    if (selectionChanged) {
      await fetchSelectedDetail(selectedId.value)
    }
    if (selectionChanged || !editConfig.value) await syncEditConfigFromSelected()
  } catch (e: unknown) {
    const err = toAppError(e)
    if (err.code === 'ERR_CANCELED') {
      loading.value = false
      return
    }
    loadError.value = {
      message: err.message || '加载设备信息失败',
      status: err.status,
      method: err.method,
      url: err.url
    }
  } finally {
    loading.value = false
  }
}

async function refreshListOnly() {
  try {
    const prevSelected = selectedId.value
    if (listAbort) listAbort.abort()
    listAbort = new AbortController()
    const listResult = await devicesStore.fetchList(listAbort.signal)
    if (!listResult.ok) throw new Error(listResult.error.message)
    devices.value = (storeList.value || []) as DeviceMgmtListItem[]
    // 自动刷新时如果当前选中的设备突然不在列表中，不要强行将其重置。
    // 这可以避免正在配置某设备时，因为网络或拔插一秒钟的掉线导致系统强制关闭当前配置并把页面顶上去拉回到第一项。
    if (!hasAutoSelected.value && !selectedId.value && devices.value.length) {
      selectedId.value = devices.value[0]?.id || ''
      hasAutoSelected.value = !!selectedId.value
    }
    const selectedStillExists = selectedId.value
      ? devices.value.some(d => d.id === selectedId.value)
      : false
    if (selectedId.value && !selectedStillExists && devices.value.length === 0) {
      selectedId.value = ''
    }
    const selectionChanged = prevSelected !== selectedId.value || (selectedDetail.value?.id || '') !== selectedId.value
    if (selectionChanged) {
      await fetchSelectedDetail(selectedId.value)
      await syncEditConfigFromSelected()
    }
    listPollFailCount.value = 0
    listPollWarned.value = false
  } catch (e: unknown) {
    const err = toAppError(e)
    if (err.code === 'ERR_CANCELED') return
    listPollFailCount.value += 1
    debugCollector.recordApiError(e)
    if (listPollFailCount.value >= 3 && !listPollWarned.value) {
      listPollWarned.value = true
      ElMessage.warning('设备列表刷新异常，已自动降低刷新频率')
    }
    throw e
  }
}

async function refreshSelectedDetailOnly() {
  if (!selectedId.value) return
  try {
    const outcome = await fetchSelectedDetail(selectedId.value)
    if (outcome.status === 'stale') return
    if (outcome.status === 'failed') throw outcome.error
    detailPollFailCount.value = 0
    detailPollWarned.value = false
  } catch (e: unknown) {
    const err = toAppError(e)
    if (err.code === 'ERR_CANCELED') return
    detailPollFailCount.value += 1
    debugCollector.recordApiError(e)
    if (detailPollFailCount.value >= 3 && !detailPollWarned.value) {
      detailPollWarned.value = true
      ElMessage.warning('设备详情刷新异常，已自动降低刷新频率')
    }
    throw e
  }
}

function retrySelectedDetail() {
  void refreshSelectedDetailOnly().catch(() => {})
}

function retryCardPolicy() {
  void fetchCardPolicy(selectedDetail.value?.modem?.iccid)
}

function retryConfig() {
  void syncEditConfigFromSelected(true)
}

async function refreshDeviceViews() {
  await Promise.all([refreshSelectedDetailOnly(), refreshListOnly()])
}

function scheduleRefreshDeviceViews(delayMs: number) {
  window.setTimeout(() => {
    void refreshDeviceViews().catch(() => {})
  }, delayMs)
}

async function selectDevice(id: string) {
  const next = String(id || '').trim()
  if (!next) return
  if (selectedId.value === next && selectedDetail.value) return
  selectedId.value = next
  hasAutoSelected.value = true
  void router.replace({
    name: 'Devices',
    query: {
      ...route.query,
      device: next,
      tab: activeTab.value
    }
  })
  await Promise.all([fetchSelectedDetail(next), syncEditConfigFromSelected()])
}

watch(
  () => [route.query.device, route.query.tab],
  () => {
    const selectionChanged = applyRouteSelection()
    if (!selectionChanged) return
    void Promise.all([
      fetchSelectedDetail(selectedId.value),
      syncEditConfigFromSelected()
    ])
  }
)

async function rotateIP() {
  const id = String(selectedId.value || '').trim()
  if (!id) return
  if (!selectedListItem.value?.network_connected) {
    ElMessage.warning('设备网络未连接，请先启动网络')
    return
  }
  const confirmed = await ElMessageBox.confirm(
    `确定对设备 ${id} 执行 IP 轮换？`,
    '确认操作',
    { confirmButtonText: '立即轮换', cancelButtonText: '取消', type: 'warning' }
  ).then(() => true).catch(() => false)
  if (!confirmed) return

  rotating.value = true
  try {
    const result = await devicesService.rotateIP(id)
    if (!result.ok) throw new Error(result.error.message || '轮换失败')
    ElMessage.success('轮换请求已发送')
    await refreshDeviceViews()
    scheduleRefreshDeviceViews(1500)
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error(err.message || '轮换失败')
  } finally {
    rotating.value = false
  }
}

async function reconnectVoWiFi() {
  const id = String(selectedId.value || '').trim()
  if (!id) return
  const confirmed = await ElMessageBox.confirm(
    `确定对设备 ${id} 发起 VoWiFi 环境的重新连接拨号？这将在后台重新注册 IMS 链路。`,
    '重连 VoWiFi',
    { confirmButtonText: '确定重连', cancelButtonText: '取消', type: 'info' }
  ).then(() => true).catch(() => false)
  if (!confirmed) return

  reconnectingVoWiFi.value = true
  try {
    const result = await devicesService.reconnectVoWiFi(id)
    if (!result.ok) throw new Error(result.error.message || '重连请求失败')
    ElMessage.success('已触发重连指令，VoWiFi 服务正在重启...')
    void refreshDeviceViews().catch(() => {})
    scheduleRefreshDeviceViews(4000)
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error(err.message || '重连命令下发失败')
  } finally {
    reconnectingVoWiFi.value = false
  }
}

async function openE911Websheet() {
  const id = String(selectedId.value || '').trim()
  if (!id || e911Starting.value) return

  e911Starting.value = true
  try {
    const result = await devicesService.startE911Websheet(id)
    if (!result.ok) throw new Error(result.error.message || 'E911地址设置页面打开失败')
    e911Websheet.value = result.data
    e911WebsheetOpen.value = true
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error(err.message || 'E911地址设置页面打开失败')
  } finally {
    e911Starting.value = false
  }
}

async function finishE911Websheet() {
  e911WebsheetOpen.value = false
  e911Websheet.value = null
  await refreshDeviceViews()
}

const rebooting = ref(false)
async function rebootModem() {
  const id = String(selectedId.value || '').trim()
  if (!id) return
  const confirmed = await ElMessageBox.confirm(
    `确定对设备 ${id} 发送重启模组指令？设备将在此期间脱网和失联数秒。`,
    '确认重启',
    { confirmButtonText: '立即重启', cancelButtonText: '取消', type: 'warning' }
  ).then(() => true).catch(() => false)
  if (!confirmed) return

  rebooting.value = true
  try {
    const result = await devicesService.rebootModem(id)
    if (!result.ok) throw new Error(result.error.message || '指令下发失败')
    ElMessage.success('重启指令已送达，设备正在重新启动')
    void refreshDeviceViews().catch(() => {})
    // 稍微延迟查询，因为网络可能正在断开
    scheduleRefreshDeviceViews(5000)
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error(err.message || '指令下发失败')
  } finally {
    rebooting.value = false
  }
}

// 手动触发设备重新扫描
async function rescanDevices() {
  rescanning.value = true
  try {
    const result = await devicesService.rescanAll()
    if (!result.ok) throw new Error(result.error.message || '重新扫描失败')
    ElMessage.success('设备重新扫描完成')
    await fetchAll()
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error(err.message || '重新扫描失败')
  } finally {
    rescanning.value = false
  }
}

function openSms() {
  if (!selectedId.value) return
  router.push(`/sms?device=${selectedId.value}`)
}

async function saveConfig() {
  const id = String(selectedId.value || '').trim()
  if (!id || !editConfig.value) return

  const baseline = editBaseline.value
    ? JSON.parse(editBaseline.value) as DeviceConfigDTO
    : null
  const previousBackend = String(baseline?.device_backend || '').toLowerCase()
  const nextBackend = String(editConfig.value.device_backend || '').toLowerCase()
	const switchingBackend = isManagedDeviceBackendSwitch(previousBackend, nextBackend)
  if (switchingBackend) {
    const confirmed = await ElMessageBox.confirm(
      `确定将设备 ${id} 从 ${previousBackend.toUpperCase()} 切换为 ${nextBackend.toUpperCase()}？模组会重启并短暂离线。`,
      '切换设备运行模式',
      { confirmButtonText: '确认切换', cancelButtonText: '取消', type: 'warning' }
    ).then(() => true).catch(() => false)
    if (!confirmed) return
  }

  saving.value = true
  try {
    const result = await devicesService.updateConfig(id, editConfig.value)
    if (!result.ok) throw new Error(result.error.message || '保存失败')
	if (result.data.backendSwitch?.workerStarted) {
	  ElMessage.success(`设备已切换为 ${result.data.backendSwitch.targetBackend.toUpperCase()} 并重新上线`)
	} else if (result.data.warning) {
      ElMessage.warning(result.data.warning)
    } else if (result.data.requiresRestart) {
      ElMessage.warning('配置已保存，但部分变更需要重启服务后生效')
    } else {
      ElMessage.success('配置已保存')
    }
    editDirty.value = false
    editBaseline.value = JSON.stringify(editConfig.value)
    await Promise.all([refreshListOnly(), refreshSelectedDetailOnly()])
    await syncEditConfigFromSelected(true)
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error(err.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function deleteDevice() {
  const id = String(selectedId.value || '').trim()
  if (!id) return
  const confirmed = await ElMessageBox.confirm(
    `确定删除设备 ${id} 的配置？删除后该设备将停止接管（代理/网络/AT）。`,
    '确认删除',
    { confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning' }
  ).then(() => true).catch(() => false)
  if (!confirmed) return

  deleting.value = true
  try {
    const result = await devicesService.deleteManaged(id)
    if (!result.ok) throw new Error(result.error.message || '删除失败')
    ElMessage.success('设备已删除')
    clearLiveRadioFallbackTimer()
    selectedDetail.value = null
    updateTrafficSpeedFromSelected()
    resetDeviceTrafficAnalysis()
    editConfig.value = null
    editBaseline.value = ''
    editDirty.value = false
    selectedId.value = ''
    hasAutoSelected.value = false
    await fetchAll()
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error(err.message || '删除失败')
  } finally {
    deleting.value = false
  }
}

function openAddDialog() {
  addDialogOpen.value = true
  addSelected.value = null
  addConfig.value = {
    id: '',
    name: '',
    interface: '',
    modem_imei: '',
    usb_path: '',
    esim_transport: 'at',
    at_port: '',
    control_device: '',
    device_backend: 'at'
  }
  refreshDiscoveredForAdd()
}

async function refreshDiscoveredForAdd() {
  discovering.value = true
  try {
    const result = await devicesStore.fetchDiscovered()
    if (result.ok) {
      discovered.value = Array.isArray(storeDiscovered.value) ? storeDiscovered.value : []
      if (pcscDiscoveryError.value) {
        ElMessage.warning(`PC/SC 读卡器探测失败: ${pcscDiscoveryError.value}`)
      }
    } else {
      ElMessage.error(result.error.message)
    }
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : String(error))
  } finally {
    discovering.value = false
  }
}

function applyDiscoveredToAddConfig(d: DiscoveredDevice | null) {
  if (!d) return
  addConfig.value.interface = d.net_interface || ''
  addConfig.value.at_port = d.at_port || ''
  addConfig.value.control_device = d.control_path || ''
  addConfig.value.modem_imei = d.imei || ''
  addConfig.value.usb_path = d.usb_path || ''

  const mode = String(d.mode || '').toLowerCase()
  if (mode === 'pcsc' || d.hardware_kind === 'pcsc') {
    addConfig.value.device_backend = 'pcsc'
    addConfig.value.esim_transport = 'pcsc'
    addConfig.value.pcsc_reader_name = d.reader_name || d.control_path || ''
    addConfig.value.pcsc_usb_path = d.usb_path || ''
    addConfig.value.modem_imei = ''
  } else if (mode === 'mbim') {
    addConfig.value.device_backend = 'mbim'
  } else if (isWwanQmiControlPath(d.control_path) || (mode === 'qmi' && d.control_path)) {
    addConfig.value.device_backend = 'qmi'
  } else {
    addConfig.value.device_backend = 'at'
  }
}

function selectDiscoveredForAdd(d: DiscoveredDevice) {
  if (d.degraded) {
    ElMessage.warning(d.hardware_kind === 'pcsc' ? '读卡器内没有可用卡片' : '无法读取该设备 IMEI（可能控制口挂死），请执行 AT!RESET 或切换组态后重试')
    return
  }
  addSelected.value = d
  applyDiscoveredToAddConfig(d)
}

async function addDevice() {
  addSaving.value = true
  try {
    if (!addSelected.value) {
      ElMessage.warning('请选择一个未配置设备')
      return
    }
    applyDiscoveredToAddConfig(addSelected.value)
    const result = await devicesService.addManaged(addConfig.value)
    if (!result.ok) throw new Error(result.error.message || '添加失败')
    const warning = result.data.warning
    const started = result.data.started
    if (warning) {
      ElMessage.warning(warning)
    } else if (started === true) {
      ElMessage.success('设备已添加并开始接管')
    } else {
      ElMessage.success('设备配置已添加')
    }
    addDialogOpen.value = false
    await fetchAll()
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error(err.message || '添加失败')
  } finally {
    addSaving.value = false
  }
}

function refreshCurrentDeviceTrafficAnalysis() {
  const detail = selectedDetail.value
  if (!detail?.id) {
    resetDeviceTrafficAnalysis()
    return
  }
  void fetchDeviceTrafficAnalysis(detail.id)
}

function handleDeviceTrafficRangeChange(range: TrafficRange) {
  if (deviceAnalysisRange.value === range) return
  deviceAnalysisRange.value = range
  refreshCurrentDeviceTrafficAnalysis()
}

watch(
  selectedId,
  (nextID, prevID) => {
    if (nextID !== prevID) {
      clearLiveRadioFallbackTimer()
      resetDeviceTrafficAnalysis()
    }
  }
)

watch(
  () => selectedDetail.value?.id || '',
  (nextID) => {
    if (nextID) {
      void fetchDeviceTrafficAnalysis(nextID)
    }
  }
)

watch(
  () => selectedDetail.value?.network_connected,
  (connected, prevConnected) => {
    const detail = selectedDetail.value
    if (!detail?.id) return
    if (!connected) {
      resetDeviceTrafficAnalysis()
      return
    }
    if (prevConnected === false && connected === true) {
      void fetchDeviceTrafficAnalysis(detail.id)
    }
  }
)

watch(activeTab, (tab, prevTab) => {
  if (tab === 'overview' && prevTab !== 'overview') {
    refreshCurrentDeviceTrafficAnalysis()
  }
})

onMounted(() => {
  fetchAll()
  getMccMncIndex().then(index => {
    mccMncIndex.value = index
  }).catch(() => {})
})

onBeforeUnmount(() => {
  if (listAbort) listAbort.abort()
  if (detailAbort) detailAbort.abort()
  if (trafficAbort) trafficAbort.abort()
  clearLiveRadioFallbackTimer()
})

// 由于 SSE 只订阅当前选中的单设备详情，恢复列表的低频拉取（无 IPC 开销）以同步左右设备增减和状态跳变。
const listEnabled = computed(() => !loading.value)
usePollingScheduler(refreshListOnly, 15000, {
  enabled: listEnabled,
  maxIntervalMs: 120000,
  backgroundIntervalMs: 45000
})

type OverviewSSEPayload = { devices?: DeviceOverviewItem[] }

function handleOverviewEvent(data: OverviewSSEPayload) {
  if (!data?.devices || !data.devices.length) return

  const found = data.devices[0]
  const idx = devices.value.findIndex(d => d.id === found.id)
  if (idx !== -1) {
    Object.assign(devices.value[idx], found)
    devices.value = [...devices.value]
  }

  if (selectedId.value === found.id) {
    selectedDetail.value = resolveDetailForDisplay(found as any)
    detailError.value = null
    detailLoading.value = false
    updateTrafficSpeedFromSelected()
  }

  loading.value = false
}

let overviewStream: ReturnType<typeof useEventStream<OverviewSSEPayload>> | null = null

function setupSSE() {
  if (overviewStream) {
    overviewStream.disconnect()
    overviewStream = null
  }
  realtimeTrafficActiveUntil.value = 0
  trafficSpeedRx.value = ''
  trafficSpeedTx.value = ''
  resetRollingTrafficWindow()

  const id = selectedId.value
  if (!id) {
    return
  }

  overviewStream = useEventStream<OverviewSSEPayload>({
    path: `/devices/${id}/overview/stream`,
    eventName: 'overview',
    reconnectDelayMs: 3000,
    parse: (payload: string) => JSON.parse(payload) as OverviewSSEPayload,
    onEvent: handleOverviewEvent,
    onRawEvent: (eventName: string, payload: string) => {
      if (eventName !== 'traffic') return
      try {
        handleRealtimeTrafficEvent(JSON.parse(payload) as RealtimeTrafficSnapshot)
      } catch {
        // Ignore a malformed realtime frame without tearing down the overview stream.
      }
    }
  })

  void overviewStream.connect()
}

watch(selectedId, () => {
  setupSSE()
})

onMounted(() => {
  setupSSE()
})

onBeforeUnmount(() => {
  overviewStream?.disconnect()
})

usePollingScheduler(async () => {
  if (activeTab.value !== 'overview') return
  if (!selectedDetail.value?.id || !selectedDetail.value.network_connected) return
  await fetchDeviceTrafficAnalysis(selectedDetail.value.id)
}, 60000, {
  immediate: false,
  maxIntervalMs: 300000,
  backgroundIntervalMs: 120000
})
</script>

<template>
  <div class="app-page devices-page">
    <div class="device-action-row">
      <div class="device-page-heading">
        <span>DEVICE MANAGEMENT</span>
        <h1>设备管理</h1>
      </div>
      <div class="device-global-actions">
        <RefreshButton :loading="loading" @click="fetchAll" />
        <el-button @click="rescanDevices" :loading="rescanning" class="ui-glass-border !border-0">
          <el-icon><ArrowSync24Regular /></el-icon>
          重新扫描
        </el-button>
        <el-button type="primary" @click="openAddDialog" class="!border-0">
          <el-icon><Add24Regular /></el-icon>
          添加设备
        </el-button>
      </div>
    </div>

    <ErrorState
      v-if="loadError"
      class="mb-6"
      title="设备数据加载失败"
      :message="loadError.message"
      :status-code="loadError.status"
      :request-method="loadError.method"
      :request-url="loadError.url"
      :last-success-at="loadLastOkAt"
      retry-text="重试"
      @retry="fetchAll"
    />

    <div class="devices-layout">
      <DeviceListPanel
        :loading="loading"
        :query="query"
        :status-filter="statusFilter"
        :sort-key="sortKey"
        :sort-dir="sortDir"
        :selected-id="selectedId"
        :filtered-devices="filteredDevices"
        :device-count="devices.length"
        :device-limit="deviceLimit"
        @update:query="query = $event"
        @update:status-filter="statusFilter = $event"
        @update:sort-key="sortKey = $event"
        @update:sort-dir="sortDir = $event"
        @select-device="selectDevice"
      />

      <main v-if="selectedDevice" class="device-workspace">
        <section class="device-workspace-shell ui-card">
          <DeviceDetailHeader
            :device="selectedDevice"
            :rotating="rotating"
            :rebooting="rebooting"
            :reconnecting="reconnectingVoWiFi"
            @open-sms="openSms"
            @rotate-ip="rotateIP"
            @reboot-modem="rebootModem"
            @reconnect-vowifi="reconnectVoWiFi"
          />
          <div class="device-workspace-surface">
            <el-tabs v-model="activeTab" class="device-detail-tabs">
              <el-tab-pane label="概览" name="overview">
              <div class="space-y-6">
                <DeviceOverviewTab
                  :device="selectedDevice"
                  :sim-operator-display="selectedSimOperatorDisplay"
                  :sim-operator-country-code="selectedSimOperatorCountryCode"
                  :traffic-speed-rx="trafficSpeedRx"
                  :traffic-speed-tx="trafficSpeedTx"
                  :traffic-minute-rx="rollingMinuteRx"
                  :traffic-minute-tx="rollingMinuteTx"
                  :e911-starting="e911Starting"
                  @setup-e911="openE911Websheet"
                />
                <TrafficAnalysisPanel
                  :analysis="deviceAnalysis"
                  :loading="deviceAnalysisLoading"
                  :error="deviceAnalysisError"
                  :last-ok-at="deviceAnalysisLastOkAt"
                  :range="deviceAnalysisRange"
                  mode="device"
                  title="当前设备流量分析"
                  subtitle="数据每分钟采样一次，按日/周/月聚合"
                  :disabled="!selectedDevice?.network_connected"
                  :device-label="selectedDevice?.name || selectedDevice?.id"
                  @update:range="handleDeviceTrafficRangeChange"
                  @refresh="refreshCurrentDeviceTrafficAnalysis"
                />
              </div>
            </el-tab-pane>
            <el-tab-pane label="eSIM" name="esim" lazy>
              <DeviceEsimTab :device-id="selectedDevice.id" :device-imei="selectedDevice.modem?.imei || ''" :is-active="activeTab === 'esim'" :device-online="selectedDevice.running === true" />
            </el-tab-pane>
            <el-tab-pane label="AT 终端" name="at" lazy>
              <DeviceAtTab
                :device-id="selectedDevice.id"
                :backend-mode="selectedDevice.backend_mode"
                :at-port="selectedDevice.at_port"
                :running="selectedDevice.running"
              />
            </el-tab-pane>
            <el-tab-pane label="USSD 终端" name="ussd" lazy>
              <DeviceUssdTab :device-id="selectedDevice.id" />
            </el-tab-pane>
            <el-tab-pane label="卡策略" name="card" lazy>
              <div v-if="cardPolicyLoading" class="tab-loading-state ui-panel-muted">
                <el-skeleton :rows="4" animated />
              </div>
              <ErrorState
                v-else-if="cardPolicyError"
                title="卡策略读取失败"
                :message="cardPolicyError.message"
                :status-code="cardPolicyError.status"
                :request-method="cardPolicyError.method"
                :request-url="cardPolicyError.url"
                retry-text="重新读取"
                @retry="retryCardPolicy"
              />
              <CardPolicyPanel
                v-else
                :device-id="selectedDevice.id"
                :iccid="selectedDetail?.modem?.iccid"
                :policy="cardPolicy"
                :device-online="selectedDevice.running === true"
                @policy-changed="onCardPolicyChanged"
              />
            </el-tab-pane>
            <el-tab-pane label="配置" name="config" lazy>
              <div v-if="configLoading" class="tab-loading-state ui-panel-muted">
                <el-skeleton :rows="5" animated />
              </div>
              <ErrorState
                v-else-if="configError"
                title="设备配置读取失败"
                :message="configError.message"
                :status-code="configError.status"
                :request-method="configError.method"
                :request-url="configError.url"
                retry-text="重新读取"
                @retry="retryConfig"
              />
              <DeviceConfigTab
                v-else
                :edit-config="editConfig"
                :device-status="selectedDetail"
                :saving="saving"
                :deleting="deleting"
                @save="saveConfig"
                @delete="deleteDevice"
              />
              </el-tab-pane>
            </el-tabs>
          </div>
        </section>
      </main>

      <main v-else class="device-workspace">
        <DeviceDetailLoading v-if="loading || detailLoading" />
        <ErrorState
          v-else-if="detailError"
          title="设备详情读取失败"
          :message="detailError.message"
          :status-code="detailError.status"
          :request-method="detailError.method"
          :request-url="detailError.url"
          retry-text="重新读取"
          @retry="retrySelectedDetail"
        />
        <section v-else class="device-workspace-empty ui-card">
          <div class="device-workspace-empty-icon" aria-hidden="true">
            <el-icon><Sim24Regular /></el-icon>
          </div>
          <span>DEVICE WORKSPACE</span>
          <h2>{{ loadError ? '设备数据不可用' : '等待设备接入' }}</h2>
          <p>{{ loadError ? '请根据上方错误信息重试真实请求' : '添加或选择设备后，可在这里管理连接、eSIM 与终端' }}</p>
        </section>
      </main>
    </div>
  </div>

  <DeviceAddDialog
    v-model="addDialogOpen"
    :discovering="discovering"
    :unconfigured-discovered="addableDiscovered"
    :add-selected="addSelected"
    :add-config="addConfig"
    :add-saving="addSaving"
    @select-device="selectDiscoveredForAdd"
    @save="addDevice"
  />
  <CarrierWebsheetDialog
    v-model="e911WebsheetOpen"
    :websheet="e911Websheet"
    @done="finishE911Websheet"
  />
</template>

<style scoped>
.devices-page {
  container-type: inline-size;
}

.tab-loading-state {
  min-height: 220px;
  padding: 28px;
  border-radius: 10px;
}

.device-action-row,
.device-global-actions {
  display: flex;
  align-items: center;
}

.device-action-row {
  min-height: 58px;
  margin-bottom: 14px;
  justify-content: space-between;
  gap: 12px;
}

.device-global-actions {
  flex-wrap: wrap;
  gap: 8px;
}

.device-page-heading span,
.device-workspace-empty > span {
  color: var(--ui-primary);
  font: 700 var(--ui-font-caption) "v-mono", ui-monospace, monospace;
  letter-spacing: .16em;
}

.device-page-heading h1 {
  margin: 3px 0 0;
  color: var(--ui-text);
  font-size: 22px;
  font-weight: 650;
  letter-spacing: -.02em;
}

.device-global-actions {
  margin-left: auto;
  justify-content: flex-end;
}

.devices-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  align-items: start;
  gap: 1rem;
}

@container (min-width: 980px) {
  .devices-layout {
    grid-template-columns: 276px minmax(0, 1fr);
  }
}

@container (max-width: 760px) {
  .device-action-row {
    align-items: flex-start;
    flex-direction: column;
  }

  .device-global-actions {
    width: 100%;
  }
}

.device-workspace {
  min-width: 0;
}

.device-workspace > .device-workspace-shell,
.device-workspace > .device-workspace-empty {
  animation: device-workspace-enter 240ms var(--ui-ease-out) both;
}

.device-workspace-shell {
  min-width: 0;
  overflow: hidden;
}

.device-workspace-surface {
  min-width: 0;
  padding: 0 22px 22px;
  overflow: hidden;
}

.device-detail-tabs :deep(.el-tabs__content) {
  overflow: visible;
}

.device-detail-tabs :deep(.el-tabs__header) {
  margin: 0 -22px 22px;
  padding: 0 22px;
  background: color-mix(in srgb, var(--ui-surface-strong) 72%, var(--ui-surface));
}

.device-detail-tabs :deep(.el-tabs__nav-wrap::after) {
  height: 1px;
  background: var(--ui-border);
}

.device-detail-tabs :deep(.el-tabs__item) {
  height: 58px;
  color: var(--ui-text-muted);
  font-size: 13px;
}

.device-detail-tabs :deep(.el-tabs__item.is-active) {
  color: var(--ui-primary);
  font-weight: 650;
}

.device-workspace-empty {
  min-height: 440px;
  padding: 48px 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  text-align: center;
  background:
    linear-gradient(145deg, color-mix(in srgb, var(--ui-primary) 5%, transparent), transparent 42%),
    var(--ui-surface);
}

.device-workspace-empty-icon {
  width: 50px;
  height: 50px;
  margin-bottom: 18px;
  display: grid;
  place-items: center;
  border: 1px solid var(--ui-border);
  border-radius: 13px;
  background: var(--ui-surface-strong);
  color: var(--ui-primary);
  font-size: 24px;
}

.device-workspace-empty h2 {
  margin: 8px 0 0;
  color: var(--ui-text);
  font-size: 20px;
  font-weight: 650;
}

.device-workspace-empty p {
  max-width: 360px;
  margin: 6px 0 0;
  color: var(--ui-text-muted);
  font-size: 12px;
}

.device-detail-tabs :deep(.el-tab-pane) {
  padding-bottom: 0.25rem;
}

@keyframes device-workspace-enter {
  from { opacity: 0; transform: translateY(8px); }
  to { opacity: 1; transform: translateY(0); }
}

@media (prefers-reduced-motion: reduce) {
  .device-workspace > .device-workspace-shell,
  .device-workspace > .device-workspace-empty {
    animation-name: device-workspace-fade;
  }

  @keyframes device-workspace-fade {
    from { opacity: 0; }
    to { opacity: 1; }
  }
}

@media (max-width: 760px) {
  .device-workspace-surface {
    padding: 0 14px 14px;
  }

  .device-workspace-empty {
    min-height: 300px;
  }

  .device-detail-tabs :deep(.el-tabs__header) {
    margin-right: -14px;
    margin-left: -14px;
    padding: 0 14px;
  }
}
</style>
