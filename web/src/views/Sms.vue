<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Loading } from '@element-plus/icons-vue'
import { useSMSStore } from '../stores/sms'
import { usePollingScheduler } from '../composables/usePollingScheduler'
import { toAppError } from '../services/http'
import type { SmsThreadQueryParams } from '../services/sms'
import EmptyState from '../components/EmptyState.vue'
import ErrorState from '../components/ErrorState.vue'
import SmsDeviceRail from '../components/sms/SmsDeviceRail.vue'
import SmsThreadListPane from '../components/sms/SmsThreadListPane.vue'
import type { DeviceMgmtListItem, SMSMessage } from '../types/api'
import { Delete24Regular, Send24Regular } from '@vicons/fluent'
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css'
import { formatDeviceDate, formatDeviceDateTime } from '../utils/deviceTime'
import { createSmsDeviceChannels } from '../utils/smsPresentation'

type SmsThread = {
  key: string
  imsi: string
  peer: string
  deviceId?: string
  lastTs: number
  lastSmsId: number
  lastMessage: string
  lastDeviceName?: string
  localPhone?: string
  peerLower: string
  lastMessageLower: string
}

const route = useRoute()
const router = useRouter()
const smsStore = useSMSStore()

const devices = ref<DeviceMgmtListItem[]>([])
const devicesLastOkAt = ref<number | null>(null)
const devicesError = ref<{ message: string; status?: number; method?: string; url?: string } | null>(null)

const loading = ref(false)
const threads = ref<SmsThread[]>([])
const messagesLastOkAt = ref<number | null>(null)
const messagesError = ref<{ message: string; status?: number; method?: string; url?: string } | null>(null)

const selectedDevice = ref<string>(typeof route.query.device === 'string' ? route.query.device : 'all')
const selectedThreadKey = ref<string>(typeof route.query.contact === 'string' ? route.query.contact : '')
const searchQuery = ref('')

const smsPageRef = ref<HTMLElement | null>(null)
const smsPageWidth = ref(0)
const SMS_NARROW_BREAKPOINT = 980
let smsPageResizeObserver: ResizeObserver | null = null

function syncSmsPageWidth() {
  smsPageWidth.value = smsPageRef.value?.clientWidth || 0
}

function parseTs(s: string) {
  const ms = new Date(s).getTime()
  return Number.isFinite(ms) ? ms : 0
}

function dateKey(timestamp: string) {
  return formatDeviceDate(timestamp, { fallback: '未知日期' })
}

function lastSeenKey(threadKey: string) {
  return `sms_thread_last_seen:${selectedDevice.value}:${threadKey}`
}

function getLastSeen(threadKey: string) {
  try {
    const v = localStorage.getItem(lastSeenKey(threadKey))
    return v ? Number(v) || 0 : 0
  } catch {
    return 0
  }
}

function setLastSeen(threadKey: string, ts: number) {
  try {
    localStorage.setItem(lastSeenKey(threadKey), String(ts || Date.now()))
  } catch {
    // Ignore storage write failures in private/sandboxed modes.
  }
}

const filteredThreads = computed(() => {
  const q = String(searchQuery.value || '').trim().toLowerCase()
  if (!q) return threads.value
  return threads.value.filter(t => {
    if (t.peerLower.includes(q)) return true
    return t.lastMessageLower.includes(q)
  })
})

const selectedThread = computed(() => {
  return threads.value.find(t => t.key === selectedThreadKey.value) || null
})

const loadingHistoryMore = ref(false)
const threadLoading = ref(false)
const threadMessages = ref<SMSMessage[]>([])
const threadHasMore = ref(false)

const canLoadMoreHistory = computed(() => {
  return !!selectedThread.value && threadHasMore.value
})

const selectedThreadGroups = computed(() => {
  if (!selectedThread.value) return []
  const out: Array<{ date: string; items: SMSMessage[] }> = []
  let last = ''
  for (const m of threadMessages.value) {
    const key = dateKey(m.timestamp)
    if (!out.length || key !== last) {
      out.push({ date: key, items: [m] })
      last = key
    } else {
      out[out.length - 1].items.push(m)
    }
  }
  return out
})

const isNarrowLayout = computed(() => smsPageWidth.value > 0 && smsPageWidth.value < SMS_NARROW_BREAKPOINT)
const showDeviceSidebar = computed(() => !isNarrowLayout.value)
const showListPane = computed(() => !isNarrowLayout.value || !selectedThreadKey.value)
const showDetailPane = computed(() => !isNarrowLayout.value || !!selectedThreadKey.value)

const showSendModal = ref(false)
const sending = ref(false)
const deletingMessageId = ref<number | null>(null)
const deletingThreadKey = ref<string | null>(null)
const supportsHover = ref(false)
const showActionSheet = ref(false)
const actionSheetTarget = ref<{ type: 'thread'; thread: SmsThread } | { type: 'message'; message: SMSMessage } | null>(null)
const composer = ref('')
const GSM7_BASIC_CHARS = new Set(Array.from(`@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞ !"#¤%&'()*+,-./0123456789:;<=>?¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà`))
const GSM7_EXT_CHARS = new Set(Array.from(`^{}\\[~]|€`))

function estimateSegments(text: string) {
  const raw = String(text || '')
  let gsm7Units = 0
  let isGSM7 = true
  for (const ch of Array.from(raw)) {
    if (GSM7_BASIC_CHARS.has(ch)) {
      gsm7Units += 1
      continue
    }
    if (GSM7_EXT_CHARS.has(ch)) {
      gsm7Units += 2
      continue
    }
    isGSM7 = false
    break
  }
  if (isGSM7) {
    const single = 160
    const multi = 153
    const parts = gsm7Units <= single ? 1 : Math.ceil(gsm7Units / multi)
    return { encoding: 'GSM7', parts, units: gsm7Units, unitName: 'septets' }
  }
  const ucs2Units = Array.from(raw).length
  const single = 70
  const multi = 67
  const parts = ucs2Units <= single ? 1 : Math.ceil(ucs2Units / multi)
  return { encoding: 'UCS2', parts, units: ucs2Units, unitName: 'chars' }
}

const composerLen = computed(() => Array.from(String(composer.value || '')).length)
const composerEstimate = computed(() => estimateSegments(String(composer.value || '')))
const detailScrollbar = ref<HTMLElement | null>(null)
const composerInput = ref<unknown>(null)

const sendForm = ref({
  device_id: '',
  phone: '',
  message: ''
})
const sendEstimate = computed(() => estimateSegments(String(sendForm.value.message || '')))

const selectedSendDeviceId = ref('')
const sendDeviceOptions = computed(() => devices.value.map(d => ({ label: `${d.name || d.id}`, value: d.id })))

const deviceSidebarItems = computed(() => createSmsDeviceChannels(devices.value))

function normalizeQueryDevice(device: string) {
  const v = String(device || '').trim()
  if (!v || v === 'all') return undefined
  return v
}

function buildSmsQuery(device: string, contact?: string) {
  const nextContact = String(contact || '').trim()
  return {
    ...route.query,
    device: normalizeQueryDevice(device),
    contact: nextContact || undefined
  }
}

function markThreadSeen(t: SmsThread | null) {
  if (!t) return
  setLastSeen(t.key, t.lastTs)
}

function isUnread(t: SmsThread) {
  return t.lastTs > getLastSeen(t.key)
}

function backToList() {
  if (!selectedThreadKey.value) return
  clearSelectedThread(true)
}

function scrollThreadToBottom() {
  const doScroll = () => {
    const wrap = detailScrollbar.value
    if (!wrap) return
    wrap.scrollTop = wrap.scrollHeight
  }
  // 第一次：DOM 更新后立即滚动
  nextTick(() => {
    requestAnimationFrame(doScroll)
    // 第二次：延迟 150ms 补偿大量消息渲染延迟
    setTimeout(doScroll, 150)
  })
}

function getDetailWrap() {
  return detailScrollbar.value
}

function isNearBottom(thresholdPx = 160) {
  const wrap = detailScrollbar.value
  if (!wrap) return true
  const distance = wrap.scrollHeight - (wrap.scrollTop + wrap.clientHeight)
  return distance <= thresholdPx
}

async function loadMoreHistory() {
  if (!canLoadMoreHistory.value) return
  if (loadingHistoryMore.value) return
  if (!selectedThread.value) return
  if (threadMessages.value.length === 0) return
  const wrap = getDetailWrap()
  const prevTop = wrap?.scrollTop || 0
  const prevHeight = wrap?.scrollHeight || 0
  loadingHistoryMore.value = true
  try {
    const oldest = threadMessages.value[0]
    const params: SmsThreadQueryParams = {
      peer: selectedThread.value.peer,
      limit: 80,
      before_ts: oldest.timestamp,
      before_id: oldest.id
    }
    if (selectedDevice.value && selectedDevice.value !== 'all') {
      params.device_id = selectedDevice.value
    } else {
      params.device_id = 'all'
      params.imsi = selectedThread.value.imsi
    }
    const result = await smsStore.fetchThread(params)
    if (!result.ok) throw new Error(result.error.message)
    const list = (result.data || []) as SMSMessage[]
    const merged = list.slice().sort((a, b) => parseTs(a.timestamp) - parseTs(b.timestamp) || a.id - b.id).concat(threadMessages.value)
    threadMessages.value = merged
    threadHasMore.value = list.length === params.limit
    await nextTick()
    requestAnimationFrame(() => {
      const w = getDetailWrap()
      if (!w) return
      const nextHeight = w.scrollHeight
      const delta = Math.max(0, nextHeight - prevHeight)
      w.scrollTop = prevTop + delta
    })
  } catch {
    // Ignore history load errors to keep current thread state unchanged.
  } finally {
    loadingHistoryMore.value = false
  }
}

function onDetailScroll(e: Event) {
  clearLongPress()
  const target = e.target as HTMLElement
  if (target && target.scrollTop <= 80) {
    loadMoreHistory()
  }
}

let devicesFetchSeq = 0
let messagesFetchSeq = 0
let threadFetchSeq = 0
let longPressTimer: ReturnType<typeof setTimeout> | null = null
let longPressStartX = 0
let longPressStartY = 0

function clearLongPress() {
  if (longPressTimer != null) {
    clearTimeout(longPressTimer)
    longPressTimer = null
  }
}

function startLongPress(target: { type: 'thread'; thread: SmsThread } | { type: 'message'; message: SMSMessage }, e: PointerEvent) {
  if (!isNarrowLayout.value) return
  if (e.pointerType === 'mouse') return
  clearLongPress()
  longPressStartX = e.clientX
  longPressStartY = e.clientY
  longPressTimer = setTimeout(() => {
    longPressTimer = null
    actionSheetTarget.value = target
    showActionSheet.value = true
    if (typeof navigator !== 'undefined' && typeof navigator.vibrate === 'function') {
      navigator.vibrate(20)
    }
  }, 450)
}

function moveLongPress(e: PointerEvent) {
  if (longPressTimer == null) return
  if (Math.abs(e.clientX - longPressStartX) > 10 || Math.abs(e.clientY - longPressStartY) > 10) {
    clearLongPress()
  }
}

function showThreadActionSheet(thread: SmsThread) {
  actionSheetTarget.value = { type: 'thread', thread }
  showActionSheet.value = true
}

function openMessageActionSheet(message: SMSMessage, e: PointerEvent) {
  startLongPress({ type: 'message', message }, e)
}

function closeActionSheet() {
  showActionSheet.value = false
  actionSheetTarget.value = null
}

async function onActionSheetDelete() {
  const target = actionSheetTarget.value
  closeActionSheet()
  if (!target) return
  if (target.type === 'thread') {
    await confirmDeleteThread(target.thread)
    return
  }
  await confirmDeleteMessage(target.message)
}

function clearSelectedThread(syncRoute = false) {
  threadFetchSeq += 1
  selectedThreadKey.value = ''
  threadMessages.value = []
  threadHasMore.value = false
  threadLoading.value = false
  if (syncRoute) {
    void router.replace({ query: buildSmsQuery(selectedDevice.value) })
  }
}

async function fetchDevices() {
  const seq = ++devicesFetchSeq
  devicesError.value = null
  const result = await smsStore.fetchDevices()
  if (seq !== devicesFetchSeq) return false
  if (result.ok) {
    devices.value = (result.data || []) as DeviceMgmtListItem[]
    devicesLastOkAt.value = Date.now()
    if (selectedDevice.value !== 'all' && !devices.value.some(d => d.id === selectedDevice.value)) {
      selectedDevice.value = 'all'
      clearSelectedThread(false)
      void router.replace({ query: buildSmsQuery('all') })
    }
    return true
  }
  devicesError.value = result.error
  return false
}

async function fetchMessages(silent = false) {
  const seq = ++messagesFetchSeq
  if (!silent) loading.value = true
  messagesError.value = null
  const wasNearBottom = isNearBottom()
  const result = await smsStore.fetchThreads(selectedDevice.value)
  if (seq !== messagesFetchSeq) return false
  if (result.ok) {
    threads.value = (result.data || []) as SmsThread[]
    messagesLastOkAt.value = Date.now()
  } else {
    messagesError.value = result.error
  }
  if (!silent) loading.value = false
  if (result.ok && selectedThreadKey.value && wasNearBottom) {
    scrollThreadToBottom()
  }
  return result.ok
}

async function fetchThreadLatest(silent = false) {
  const t = selectedThread.value
  if (!t) {
    threadMessages.value = []
    threadHasMore.value = false
    return false
  }
  const seq = ++threadFetchSeq
  if (!silent) threadLoading.value = true
  const params: SmsThreadQueryParams = { peer: t.peer, limit: 80 }
  if (selectedDevice.value && selectedDevice.value !== 'all') {
    params.device_id = selectedDevice.value
  } else {
    params.device_id = 'all'
    params.imsi = t.imsi
  }
  const result = await smsStore.fetchThread(params)
  if (seq !== threadFetchSeq) return false
  if (result.ok) {
    threadMessages.value = (result.data || []) as SMSMessage[]
    threadHasMore.value = threadMessages.value.length === params.limit
  } else {
    messagesError.value = result.error
  }
  if (!silent) threadLoading.value = false
  return result.ok
}

async function ensureThreadSelection(options: { syncRoute?: boolean; silent?: boolean; scrollToBottom?: boolean } = {}) {
  const syncRoute = options.syncRoute === true
  const silent = options.silent === true
  const scrollToBottom = options.scrollToBottom === true

  const current = selectedThread.value
  if (current) {
    const ok = await fetchThreadLatest(silent)
    if (ok) {
      markThreadSeen(current)
      if (scrollToBottom) scrollThreadToBottom()
    }
    return
  }

  threadMessages.value = []
  threadHasMore.value = false
  if (selectedThreadKey.value) {
    selectedThreadKey.value = ''
    if (syncRoute) {
      void router.replace({ query: buildSmsQuery(selectedDevice.value) })
    }
  }
  if (isNarrowLayout.value || filteredThreads.value.length === 0) return
  await selectThread(filteredThreads.value[0].key, { syncRoute, silent, scrollToBottom })
}

async function selectThread(key: string, options: { syncRoute?: boolean; silent?: boolean; scrollToBottom?: boolean } = {}) {
  if (!key) return
  if (selectedThreadKey.value === key && threadMessages.value.length > 0) return

  const syncRoute = options.syncRoute !== false
  const silent = options.silent === true
  const scrollToBottom = options.scrollToBottom !== false

  selectedThreadKey.value = key
  if (syncRoute) {
    void router.replace({ query: buildSmsQuery(selectedDevice.value, key) })
  }

  const t = threads.value.find(x => x.key === key) || null
  if (!t) {
    threadMessages.value = []
    threadHasMore.value = false
    return
  }

  const ok = await fetchThreadLatest(silent)
  if (!ok) return
  markThreadSeen(t)
  if (scrollToBottom) scrollThreadToBottom()
}

async function handleSelectDevice(deviceId: string, options: { syncRoute?: boolean; silent?: boolean } = {}) {
  const nextDevice = String(deviceId || 'all').trim() || 'all'
  const syncRoute = options.syncRoute !== false
  const silent = options.silent === true

  selectedDevice.value = nextDevice
  clearSelectedThread(false)
  if (syncRoute) {
    void router.replace({ query: buildSmsQuery(nextDevice) })
  }

  const ok = await fetchMessages(silent)
  if (!ok || selectedDevice.value !== nextDevice) return
  await ensureThreadSelection({ syncRoute, silent, scrollToBottom: false })
}

async function fetchMessagesAndThread(silent = false) {
  const ok = await fetchMessages(silent)
  if (!ok) return
  await ensureThreadSelection({ syncRoute: false, silent, scrollToBottom: !silent })
}

async function refreshAll() {
  await fetchDevices()
  await fetchMessagesAndThread()
}

async function pollRefresh() {
  if (loading.value || threadLoading.value || loadingHistoryMore.value) return
  try {
    await fetchMessagesAndThread(true)
  } catch {
    // Ignore polling failures; scheduler keeps retrying.
  }
}

usePollingScheduler(pollRefresh, 5000, {
  maxIntervalMs: 60000,
  backgroundIntervalMs: 15000
})

watch(
  () => isNarrowLayout.value,
  async (isNarrow) => {
    if (isNarrow) return
    if (selectedThreadKey.value) return
    if (filteredThreads.value.length === 0) return
    await selectThread(filteredThreads.value[0].key, { syncRoute: true, scrollToBottom: false })
  }
)

onMounted(async () => {
  if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
    supportsHover.value = window.matchMedia('(hover: hover) and (pointer: fine)').matches
  }
  syncSmsPageWidth()
  if (typeof ResizeObserver !== 'undefined') {
    smsPageResizeObserver = new ResizeObserver(() => {
      syncSmsPageWidth()
    })
    if (smsPageRef.value) {
      smsPageResizeObserver.observe(smsPageRef.value)
    }
  } else {
    window.addEventListener('resize', syncSmsPageWidth, { passive: true })
  }
  const initialDevice = selectedDevice.value
  const [, messagesOk] = await Promise.all([fetchDevices(), fetchMessages()])
  if (!messagesOk && selectedDevice.value !== initialDevice) {
    await fetchMessages()
  }
  await ensureThreadSelection({ syncRoute: false, silent: false, scrollToBottom: false })
})

onUnmounted(() => {
  clearLongPress()
  smsPageResizeObserver?.disconnect()
  smsPageResizeObserver = null
  window.removeEventListener('resize', syncSmsPageWidth)
})

function openSendModal() {
  sendForm.value.phone = ''
  sendForm.value.message = ''
  selectedSendDeviceId.value = selectedDevice.value !== 'all' ? selectedDevice.value : (devices.value[0]?.id || '')
  showSendModal.value = true
}

async function handleSendModal() {
  if (!selectedSendDeviceId.value || !sendForm.value.phone || !sendForm.value.message) {
    ElMessage.warning('请填写完整信息')
    return
  }
  sending.value = true
  try {
    const result = await smsStore.send({
      device_id: selectedSendDeviceId.value,
      phone: sendForm.value.phone,
      message: sendForm.value.message
    })
    if (!result.ok) throw new Error(result.error.message || '发送失败')
    const parts = result.data.partsTotal
    ElMessage.success(`短信已发送${parts > 1 ? `（${parts}段）` : ''}`)
    showSendModal.value = false
    setTimeout(async () => {
      await fetchMessagesAndThread()
    }, 800)
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error('发送失败：' + (err.message || '未知错误'))
  } finally {
    sending.value = false
  }
}

async function sendToCurrentThread() {
  const t = selectedThread.value
  if (!t) return
  const text = String(composer.value || '').trim()
  if (!text) return
  const resolvedDeviceId = selectedDevice.value !== 'all' ? selectedDevice.value : (t.deviceId || devices.value[0]?.id || '')
  if (!resolvedDeviceId) {
    ElMessage.warning('暂无可用设备')
    return
  }
  sending.value = true
  try {
    if (selectedDevice.value === 'all') {
      const result = await smsStore.send({ imsi: t.imsi, phone: t.peer, message: text })
      if (!result.ok) throw new Error(result.error.message || '发送失败')
    } else {
      const result = await smsStore.send({ device_id: resolvedDeviceId, phone: t.peer, message: text })
      if (!result.ok) throw new Error(result.error.message || '发送失败')
    }
    composer.value = ''
    scrollThreadToBottom()
    setTimeout(async () => {
      await fetchMessagesAndThread()
    }, 800)
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error('发送失败：' + (err.message || '未知错误'))
  } finally {
    sending.value = false
  }
}

async function confirmDeleteMessage(message: SMSMessage) {
  if (!message.id || deletingMessageId.value === message.id) return
  try {
    await ElMessageBox.confirm(
      '删除后无法恢复。仅删除短信中心历史记录。',
      '删除这条短信？',
      {
        confirmButtonText: '删除',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )
  } catch {
    return
  }

  deletingMessageId.value = message.id
  try {
    const result = await smsStore.deleteMessage(message.id)
    if (!result.ok) throw new Error(result.error.message || '删除失败')
    ElMessage.success('已删除短信')
    await fetchMessagesAndThread()
    if (result.data.thread_empty) {
      clearSelectedThread(true)
    }
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error('删除失败：' + (err.message || '未知错误'))
  } finally {
    deletingMessageId.value = null
  }
}

async function confirmDeleteThread(thread: SmsThread) {
  if (deletingThreadKey.value === thread.key) return
  try {
    await ElMessageBox.confirm(
      `将删除与 ${thread.peer} 的全部短信历史，无法恢复。仅删除短信中心历史记录。`,
      '永久删除整个对话？',
      {
        confirmButtonText: '删除对话',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )
  } catch {
    return
  }

  deletingThreadKey.value = thread.key
  try {
    const payload = selectedDevice.value !== 'all'
      ? { device_id: selectedDevice.value, peer: thread.peer }
      : { device_id: 'all', imsi: thread.imsi, peer: thread.peer }
    const result = await smsStore.deleteThread(payload)
    if (!result.ok) throw new Error(result.error.message || '删除失败')
    ElMessage.success('已删除对话')
    const deletingCurrent = selectedThreadKey.value === thread.key
    if (deletingCurrent) clearSelectedThread(true)
    await fetchMessages(false)
  } catch (e: unknown) {
    const err = toAppError(e)
    ElMessage.error('删除失败：' + (err.message || '未知错误'))
  } finally {
    deletingThreadKey.value = null
  }
}

</script>

<template>
  <div ref="smsPageRef" class="app-page sms-page h-[calc(100vh-144px)] flex flex-col">
    <ErrorState
      v-if="devicesError"
      class="mb-4"
      title="设备列表加载失败"
      :message="devicesError.message"
      :status-code="devicesError.status"
      :request-method="devicesError.method"
      :request-url="devicesError.url"
      :last-success-at="devicesLastOkAt"
      retry-text="重试"
      @retry="fetchDevices"
    />

    <ErrorState
      v-if="messagesError"
      class="mb-6"
      title="短信加载失败"
      :message="messagesError.message"
      :status-code="messagesError.status"
      :request-method="messagesError.method"
      :request-url="messagesError.url"
      :last-success-at="messagesLastOkAt"
      retry-text="重试"
      @retry="refreshAll"
    />

    <div class="flex-1 sms-workspace overflow-hidden relative">
      <div v-if="loading && threads.length === 0" class="absolute inset-0 z-20 flex items-center justify-center bg-white/70 dark:bg-black/40">
        <el-icon class="is-loading" size="28"><Loading /></el-icon>
      </div>

      <div class="sms-main-layout">
        <SmsDeviceRail
          v-if="showDeviceSidebar"
          :items="deviceSidebarItems"
          :selected-id="selectedDevice"
          @select="(deviceId) => void handleSelectDevice(deviceId)"
        />

        <SmsThreadListPane
          v-if="showListPane"
          v-model="searchQuery"
          :items="filteredThreads"
          :selected-key="selectedThreadKey"
          :loading="loading"
          :deleting-key="deletingThreadKey"
          @select="(key) => void selectThread(key)"
          @delete="(thread) => void confirmDeleteThread(thread)"
          @open-actions="showThreadActionSheet"
          @new-message="openSendModal"
        />

        <section v-if="showDetailPane" class="sms-conversation-pane flex flex-col min-w-0 min-h-0">
          <div class="sms-conversation-header flex items-center justify-between gap-3">
            <div class="min-w-0">
              <div class="flex items-center gap-2">
                <el-button v-if="isNarrowLayout && selectedThreadKey" text @click="backToList">返回</el-button>
                <div class="text-sm font-extrabold text-gray-900 dark:text-white truncate">
                  {{ selectedThread?.peer || '请选择会话' }}
                </div>
              </div>
              <div class="text-xs text-gray-400 mt-1">
                {{
                  selectedDevice === 'all'
                    ? (selectedThread?.localPhone || selectedThread?.lastDeviceName
                        ? `本机：${selectedThread?.localPhone || selectedThread?.lastDeviceName}`
                        : '全部设备')
                    : `设备：${selectedDevice}`
                }}
              </div>
            </div>
            <div v-if="selectedThread" class="flex items-center gap-2">

              <el-button text @click="scrollThreadToBottom">最新</el-button>
            </div>
          </div>

          <div v-if="!selectedThread" class="flex-1 flex items-center justify-center p-6">
            <EmptyState title="请选择一个会话" subtitle="从左侧联系人列表进入短信明细" />
          </div>

          <div v-else ref="detailScrollbar" class="flex-1 min-h-0 overflow-y-auto sms-detail-scroll" @scroll="onDetailScroll">
            <div class="p-5 space-y-5">
              <div v-if="canLoadMoreHistory" class="flex justify-center">
                <el-button text type="primary" :loading="loadingHistoryMore" @click="loadMoreHistory">加载更多</el-button>
              </div>
              <div v-for="g in selectedThreadGroups" :key="g.date" class="space-y-4">
                <div class="flex justify-center">
                  <div class="text-[11px] font-bold text-gray-500 dark:text-gray-300 bg-gray-100/80 dark:bg-white/5 border border-gray-200/60 dark:border-white/10 px-3 py-1 rounded-full">
                    {{ g.date }}
                  </div>
                </div>
                <div v-for="m in g.items" :key="m.id" class="flex" :class="m.type === 1 ? 'justify-start' : 'justify-end'">
                  <div
                    class="sms-msg-wrapper group"
                    @pointerdown="(e) => openMessageActionSheet(m, e)"
                    @pointermove="moveLongPress"
                    @pointerup="clearLongPress"
                    @pointercancel="clearLongPress"
                  >
                    <div class="flex items-center gap-2 mb-1" :class="m.type === 1 ? '' : 'justify-end'">
                      <span v-if="m.type === 1" class="text-xs font-bold text-gray-700 dark:text-gray-200">{{ m.sender }}</span>
                      <el-button
                        v-if="!isNarrowLayout && m.type === 2 && m.device_name"
                        text
                        class="sms-danger-ghost-btn sms-delete-trigger sms-message-delete-btn"
                        size="small"
                        :class="{ 'sms-delete-visible': !supportsHover }"
                        :loading="deletingMessageId === m.id"
                        :aria-label="`删除短信 ${m.id}`"
                        title="删除短信"
                        @click="void confirmDeleteMessage(m)"
                      >
                        <el-icon><Delete24Regular /></el-icon>
                      </el-button>
                      <span v-if="m.device_name" class="text-[10px] font-bold text-gray-400 bg-gray-100 dark:bg-white/5 px-1.5 py-0.5 rounded">
                        {{ m.device_name }}
                      </span>
                      <span class="text-[11px] text-gray-400 font-mono">{{ formatDeviceDateTime(m.timestamp) }}</span>
                      <span v-if="m.type === 2 && m.status === 2" class="text-green-500 text-xs" title="发送成功">✓</span>
                      <span v-else-if="m.type === 2 && m.status === 3" class="text-red-500 text-xs" title="发送失败">✗</span>
                      <el-button
                        v-if="!isNarrowLayout && (m.type !== 2 || !m.device_name)"
                        text
                        class="sms-danger-ghost-btn sms-delete-trigger sms-message-delete-btn"
                        size="small"
                        :class="{ 'sms-delete-visible': !supportsHover }"
                        :loading="deletingMessageId === m.id"
                        :aria-label="`删除短信 ${m.id}`"
                        title="删除短信"
                        @click="void confirmDeleteMessage(m)"
                      >
                        <el-icon><Delete24Regular /></el-icon>
                      </el-button>
                    </div>
                    <div
                      class="px-5 py-4 rounded-lg text-sm leading-[1.75] shadow-sm border"
                      :class="m.type === 1
                        ? 'bg-white/90 dark:bg-white/5 text-gray-700 dark:text-gray-200 border-gray-100 dark:border-white/10'
                        : 'bg-blue-50 dark:bg-blue-500/10 text-gray-800 dark:text-gray-100 border-blue-100 dark:border-blue-500/20'"
                    >
                      {{ m.content }}
                    </div>
                  </div>
                </div>
              </div>
              <div class="h-2" />
            </div>
          </div>

          <div v-if="selectedThread" class="p-4 border-t border-gray-100 dark:border-white/10">
            <div class="text-[11px] text-gray-400 text-left mb-2">
              {{ composerEstimate.encoding }} · 预计 {{ composerEstimate.parts }} 段 · {{ composerLen }} 字
            </div>
            <div class="flex items-end gap-3">
              <el-input
                ref="composerInput"
                v-model="composer"
                type="textarea"
                :autosize="{ minRows: 1, maxRows: 6 }"
                resize="none"
                placeholder="回复（Enter 发送）"
                @keydown.enter.exact.prevent="sendToCurrentThread"
              />
              <el-button type="primary" :loading="sending" @click="sendToCurrentThread" class="!border-0 self-end">
                <el-icon><Send24Regular /></el-icon>
                发送
              </el-button>
            </div>
          </div>
        </section>
      </div>
    </div>

    <transition name="sms-sheet-fade">
      <div v-if="showActionSheet && isNarrowLayout" class="sms-action-sheet-mask" @click="closeActionSheet">
        <div class="sms-action-sheet" @click.stop>
          <div class="sms-action-sheet-title">操作</div>
          <el-button class="sms-danger-ghost-btn !w-full !justify-center" @click="void onActionSheetDelete()">
            <el-icon><Delete24Regular /></el-icon>
            {{ actionSheetTarget?.type === 'thread' ? '删除对话' : '删除短信' }}
          </el-button>
          <el-button class="!w-full !justify-center" @click="closeActionSheet">取消</el-button>
        </div>
      </div>
    </transition>

    <!-- Send Modal -->
    <el-dialog v-model="showSendModal" title="发送短信" width="min(520px, 92vw)" class="glass-modal">
      <el-form label-position="top" class="mt-2">
        <el-form-item label="发送设备">
          <el-select v-model="selectedSendDeviceId" placeholder="选择设备">
            <el-option v-for="opt in sendDeviceOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="目标号码">
          <el-input v-model="sendForm.phone" placeholder="+86138..." />
        </el-form-item>
        <el-form-item label="短信内容">
          <el-input
            v-model="sendForm.message"
            type="textarea"
            placeholder="输入短信内容..."
            :autosize="{ minRows: 4, maxRows: 10 }"
            resize="none"
          />
          <div class="mt-2 text-xs flex justify-end text-gray-400">
            {{ sendEstimate.encoding }} · 预计 {{ sendEstimate.parts }} 段 · {{ Array.from(String(sendForm.message || '')).length }} 字
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <el-button @click="showSendModal = false">取消</el-button>
          <el-button type="primary" :loading="sending" @click="handleSendModal">
            <el-icon><Send24Regular /></el-icon>
            发送
          </el-button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.sms-page {
  container-type: inline-size;
}

.sms-workspace {
  min-height: 0;
  border: 1px solid var(--ui-border);
  border-radius: 14px;
  background: var(--ui-surface);
  box-shadow: var(--ui-shadow-sm);
  animation: sms-workspace-enter 220ms var(--ui-ease-out) both;
}

.sms-main-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 0;
  height: 100%;
  min-height: 0;
}

.sms-conversation-pane {
  overflow: hidden;
  background: var(--ui-surface);
}

.sms-conversation-header {
  min-height: 64px;
  padding: 13px 15px;
  border-bottom: 1px solid var(--ui-border);
}

@keyframes sms-workspace-enter {
  from { opacity: 0; transform: translateY(6px); }
  to { opacity: 1; transform: translateY(0); }
}

@media (prefers-reduced-motion: reduce) {
  .sms-workspace { animation-name: sms-workspace-fade; }
  @keyframes sms-workspace-fade { from { opacity: 0; } to { opacity: 1; } }
}

.sms-msg-wrapper {
  width: 100%;
  max-width: min(620px, 88%);
}

.sms-delete-trigger {
  width: 28px;
  height: 28px;
  padding: 0;
}

.sms-message-delete-btn {
  flex: 0 0 auto;
}

.sms-delete-visible {
  opacity: 1 !important;
  pointer-events: auto !important;
}

.sms-action-sheet-mask {
  position: fixed;
  inset: 0;
  z-index: 2200;
  background: rgba(15, 23, 42, 0.36);
  display: flex;
  align-items: flex-end;
  justify-content: center;
}

.sms-action-sheet {
  width: min(520px, 100%);
  background: var(--ui-surface-strong);
  border-top-left-radius: 8px;
  border-top-right-radius: 8px;
  border: 1px solid var(--ui-border);
  border-bottom: none;
  box-shadow: var(--ui-shadow-lg);
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.sms-action-sheet-title {
  font-size: 12px;
  font-weight: 700;
  color: rgb(107 114 128);
  text-align: center;
  letter-spacing: 0.03em;
}

.sms-sheet-fade-enter-active,
.sms-sheet-fade-leave-active {
  transition: opacity 0.18s ease;
}

.sms-sheet-fade-enter-from,
.sms-sheet-fade-leave-to {
  opacity: 0;
}

:deep(.sms-danger-ghost-btn.el-button) {
  color: rgb(185 28 28);
  border-color: rgba(239, 68, 68, 0.24);
  background: rgba(239, 68, 68, 0.09);
}

:deep(.sms-danger-ghost-btn.el-button:hover) {
  color: rgb(153 27 27);
  border-color: rgba(239, 68, 68, 0.38);
  background: rgba(239, 68, 68, 0.15);
}

:deep(.sms-danger-ghost-btn.el-button:focus-visible) {
  outline: 2px solid rgba(239, 68, 68, 0.25);
  outline-offset: 2px;
}

@container (min-width: 980px) {
  .sms-main-layout {
    grid-template-columns: 218px 310px minmax(0, 1fr);
  }

  .sms-delete-trigger {
    opacity: 0;
    pointer-events: none;
    transition: opacity 0.18s ease, transform 0.2s ease;
  }

  .group:hover .sms-delete-trigger,
  .group:focus-within .sms-delete-trigger {
    opacity: 1;
    pointer-events: auto;
  }

}

@container (max-width: 979px) {
  .sms-msg-wrapper {
    max-width: 96%;
  }
}

/* 消息详情区域原生滚动条样式 */
.sms-detail-scroll::-webkit-scrollbar {
  width: 6px;
}
.sms-detail-scroll::-webkit-scrollbar-track {
  background: transparent;
}
.sms-detail-scroll::-webkit-scrollbar-thumb {
  background: rgba(144, 147, 153, 0.3);
  border-radius: 3px;
}
.sms-detail-scroll::-webkit-scrollbar-thumb:hover {
  background: rgba(144, 147, 153, 0.5);
}
/* Firefox */
.sms-detail-scroll {
  scrollbar-width: thin;
  scrollbar-color: rgba(144, 147, 153, 0.3) transparent;
}
</style>
