<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { storeToRefs } from 'pinia'
import { useRouter } from 'vue-router'
import DeviceCard from '../components/DeviceCard.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import ListSkeleton from '../components/ListSkeleton.vue'
import ErrorState from '../components/ErrorState.vue'
import RefreshButton from '../components/RefreshButton.vue'
import TrafficAnalysisPanel from '../components/TrafficAnalysisPanel.vue'
import { usePollingScheduler } from '../composables/usePollingScheduler'
import { useDashboardStore } from '../stores/dashboard'
import type { TrafficRange } from '../services/traffic'
import type { DashboardDevice } from '../types/api'
import { formatDeviceTime } from '../utils/deviceTime'
import { Search } from '@element-plus/icons-vue'
import {
  CheckmarkCircle24Regular,
  Clock24Regular,
  DismissCircle24Regular,
  Server24Regular
} from '@vicons/fluent'

const dashboard = useDashboardStore()
const router = useRouter()
const {
  devices,
  devicesLoading: loading,
  devicesLastOkAt,
  devicesError,
  analysis,
  analysisLoading,
  analysisLastOkAt,
  analysisError
} = storeToRefs(dashboard)

const analysisRange = ref<TrafficRange>('day')
const searchQuery = ref('')
const statusFilter = ref<'all' | 'online' | 'offline'>('all')
const selectedDeviceID = ref('')

const totalCount = computed(() => devices.value.length)
const onlineCount = computed(() => devices.value.filter(d => d?.healthy).length)
const offlineCount = computed(() => Math.max(0, totalCount.value - onlineCount.value))
const filteredDevices = computed(() => {
  const query = searchQuery.value.trim().toLocaleLowerCase()
  return devices.value.filter((device) => {
    if (statusFilter.value === 'online' && !device.healthy) return false
    if (statusFilter.value === 'offline' && device.healthy) return false
    if (!query) return true
    return [device.id, device.name, device.operator, device.public_ip, device.public_ipv6]
      .some((value) => String(value || '').toLocaleLowerCase().includes(query))
  })
})
const selectedDevice = computed(() => {
  return devices.value.find((device) => device.id === selectedDeviceID.value)
    || devices.value.find((device) => device.healthy)
    || devices.value[0]
})
const selectedDeviceIP = computed(() => selectedDevice.value?.public_ipv6 || selectedDevice.value?.public_ip || '')
const selectedRuntime = computed(() => selectedDevice.value?.vowifi_runtime)
const selectedConnectionTitle = computed(() => {
  const device = selectedDevice.value
  if (!device) return '等待设备'
  if (!device.healthy) return '设备离线'
  if (device.vowifi_active) return 'Wi-Fi Calling'
  return device.operator || '网络检测中'
})
const selectedConnectionState = computed(() => {
  const device = selectedDevice.value
  if (!device) return '没有可用设备'
  if (!device.healthy) return '当前设备不可用'
  return device.vowifi_active ? '已连接' : ([device.network_duplex, device.network_mode].filter(Boolean).join(' ') || '控制面在线')
})
const selectedStages = computed(() => {
  const runtime = selectedRuntime.value
  return [
    { key: 'SIM', ready: runtime?.sim_ready },
    { key: 'Access', ready: runtime?.access_ready },
    { key: 'Tunnel', ready: runtime?.tunnel_ready },
    { key: 'IMS', ready: runtime?.ims_ready },
    { key: 'SMS', ready: runtime?.sms_ready }
  ]
})

async function fetchDevices() {
  await dashboard.fetchDevices()
}

async function fetchTrafficAnalysis() {
  await dashboard.fetchAnalysis(analysisRange.value)
}

function handleAnalysisRangeChange(range: TrafficRange) {
  if (analysisRange.value === range) return
  analysisRange.value = range
  void fetchTrafficAnalysis()
}

function openDeviceOverview(id: string) {
  const deviceID = String(id || '').trim()
  if (!deviceID) return
  void router.push({
    name: 'Devices',
    query: {
      device: deviceID,
      tab: 'overview'
    }
  })
}

function selectDevice(device: DashboardDevice) {
  selectedDeviceID.value = device.id
}

usePollingScheduler(fetchDevices, 5000, {
  immediate: true,
  maxIntervalMs: 30000,
  backgroundIntervalMs: 15000
})
usePollingScheduler(fetchTrafficAnalysis, 60000, {
  immediate: false,
  maxIntervalMs: 300000,
  backgroundIntervalMs: 120000
})

onMounted(() => {
  const win = window as Window & {
    requestIdleCallback?: (cb: IdleRequestCallback, opts?: IdleRequestOptions) => number
  }
  if (typeof win.requestIdleCallback === 'function') {
    win.requestIdleCallback(() => fetchTrafficAnalysis(), { timeout: 1500 })
  } else {
    setTimeout(fetchTrafficAnalysis, 800)
  }
})
</script>

<template>
  <div class="app-page dashboard-page">
    <PageHeader title="连接总览" subtitle="统一查看全部通信设备、VoWiFi 链路与出口流量">
      <template #actions>
        <RefreshButton :loading="loading" @click="fetchDevices" />
      </template>
    </PageHeader>

    <Transition name="focus-swap" mode="out-in">
    <section v-if="selectedDevice" :key="selectedDevice.id" class="connection-stage" aria-label="当前设备连接焦点">
      <div class="connection-stage-main">
        <div class="connection-stage-heading">
          <span class="dashboard-eyebrow">ACTIVE DEVICE</span>
          <span class="focus-device-status" :class="selectedDevice.healthy ? 'is-online' : 'is-offline'">
            {{ selectedDevice.healthy ? '在线' : '离线' }}
          </span>
        </div>
        <h2>{{ selectedConnectionTitle }}</h2>
        <strong>{{ selectedConnectionState }}</strong>
        <p>{{ selectedDevice.name || selectedDevice.id }} · {{ selectedDevice.id }}</p>

        <div v-if="selectedDevice.vowifi_active" class="connection-path" aria-label="VoWiFi 服务链路">
          <div class="connection-path-line" aria-hidden="true" />
          <div
            v-for="stage in selectedStages"
            :key="stage.key"
            class="connection-path-step"
            :class="{ 'is-ready': stage.ready === true, 'is-failed': stage.ready === false }"
          >
            <span>{{ stage.ready === true ? '✓' : stage.ready === false ? '×' : '·' }}</span>
            <small>{{ stage.key }}</small>
          </div>
        </div>
        <div v-else class="cellular-focus">
          <span>{{ [selectedDevice.network_duplex, selectedDevice.network_mode].filter(Boolean).join(' ') || '未驻网' }}</span>
          <strong>{{ selectedDevice.signal_dbm || '--' }}<small v-if="selectedDevice.signal_dbm"> dBm</small></strong>
        </div>
      </div>

      <aside class="connection-stage-aside">
        <div class="focus-stat">
          <span>连接类型</span>
          <strong>{{ selectedDevice.vowifi_active ? 'VoWiFi' : (selectedDevice.network_mode || '--') }}</strong>
        </div>
        <div class="focus-stat">
          <span>运营商</span>
          <strong>{{ selectedDevice.operator || '--' }}</strong>
        </div>
        <div class="focus-stat focus-stat-ip">
          <span>公网 IP</span>
          <strong :title="selectedDeviceIP">{{ selectedDeviceIP || '--' }}</strong>
        </div>
        <button type="button" class="focus-open-button" @click="openDeviceOverview(selectedDevice.id)">
          打开设备工作区
        </button>
      </aside>
    </section>
    </Transition>

    <section class="fleet-summary" aria-label="设备状态摘要">
      <div class="fleet-summary-copy">
        <span class="section-kicker">FLEET SUMMARY</span>
        <strong>{{ onlineCount }} / {{ totalCount }}</strong>
        <span>台设备在线</span>
      </div>
      <div class="fleet-metrics">
        <div class="fleet-metric">
          <el-icon><Server24Regular /></el-icon>
          <span>全部</span>
          <strong>{{ totalCount }}</strong>
        </div>
        <div class="fleet-metric">
          <el-icon><CheckmarkCircle24Regular /></el-icon>
          <span>在线</span>
          <strong>{{ onlineCount }}</strong>
        </div>
        <div class="fleet-metric">
          <el-icon><DismissCircle24Regular /></el-icon>
          <span>离线</span>
          <strong>{{ offlineCount }}</strong>
        </div>
        <div class="fleet-metric fleet-metric-time">
          <el-icon><Clock24Regular /></el-icon>
          <span>更新</span>
          <strong>
            {{ devicesLastOkAt ? formatDeviceTime(devicesLastOkAt, { clientClock: true }) : '--:--:--' }}
          </strong>
        </div>
      </div>
    </section>

    <ErrorState
      v-if="devicesError"
      class="mb-6"
      title="设备列表加载失败"
      :message="devicesError.message"
      :status-code="devicesError.status"
      :request-method="devicesError.method"
      :request-url="devicesError.url"
      :last-success-at="devicesLastOkAt"
      retry-text="重试"
      @retry="fetchDevices"
    />

    <ListSkeleton v-if="loading && devices.length === 0" :rows="10" />

    <EmptyState v-else-if="devices.length === 0" title="暂无设备接入" subtitle="请先在设备管理中添加或接管设备" />

    <template v-else>
      <section class="device-overview-toolbar" aria-label="设备筛选">
        <div>
          <span class="section-kicker">DEVICE FLEET</span>
          <h2>设备连接</h2>
          <p>显示 {{ filteredDevices.length }} / {{ totalCount }} 台设备</p>
        </div>
        <div class="device-filter-controls">
          <el-input v-model="searchQuery" clearable placeholder="搜索设备、运营商或 IP" :prefix-icon="Search" />
          <el-segmented
            v-model="statusFilter"
            :options="[
              { label: '全部', value: 'all' },
              { label: '在线', value: 'online' },
              { label: '离线', value: 'offline' }
            ]"
          />
        </div>
      </section>

      <EmptyState
        v-if="filteredDevices.length === 0"
        title="没有匹配的设备"
        subtitle="请调整搜索关键词或在线状态筛选"
      />
      <section v-else class="device-status-grid" aria-label="设备实时状态">
        <DeviceCard
          v-for="(dev, index) in filteredDevices"
          :key="dev.id"
          :device="dev"
          :selected="selectedDevice?.id === dev.id"
          :style="{ '--device-index': index }"
          @click="selectDevice(dev)"
          @dblclick="openDeviceOverview(dev.id)"
        />
      </section>
    </template>

    <TrafficAnalysisPanel
      v-if="devices.length > 0 || !loading"
      class="mt-6"
      :analysis="analysis"
      :loading="analysisLoading"
      :error="analysisError"
      :last-ok-at="analysisLastOkAt"
      :range="analysisRange"
      mode="global"
      @update:range="handleAnalysisRangeChange"
      @refresh="fetchTrafficAnalysis"
    />
  </div>
</template>

<style scoped>
.dashboard-eyebrow, .section-kicker { color: var(--ui-primary); font: 700 10px "v-mono", monospace; letter-spacing: .14em; }
.connection-stage { position: relative; min-height: 410px; margin-bottom: 18px; padding: clamp(26px, 4vw, 54px); display: grid; grid-template-columns: minmax(0, 1fr) 300px; gap: 44px; overflow: hidden; border: 1px solid var(--ui-border); border-radius: 26px; background: radial-gradient(circle at 54% 52%, color-mix(in srgb, var(--ui-primary) 15%, transparent), transparent 30%), linear-gradient(125deg, var(--ui-surface) 0 48%, color-mix(in srgb, var(--ui-surface) 88%, #06120e) 100%); }
.connection-stage::before, .connection-stage::after { position: absolute; pointer-events: none; content: ""; }
.connection-stage::before { inset: 0 24% 0 36%; opacity: .42; background-image: radial-gradient(circle, color-mix(in srgb, var(--ui-primary) 48%, transparent) 1px, transparent 1.4px); background-size: 20px 20px; mask-image: radial-gradient(ellipse, #000, transparent 69%); }
.connection-stage::after { top: 50%; left: 42%; width: 38%; height: 1px; background: linear-gradient(90deg, transparent, color-mix(in srgb, var(--ui-primary) 48%, transparent), transparent); box-shadow: 0 -38px 0 color-mix(in srgb, var(--ui-primary) 10%, transparent), 0 38px 0 color-mix(in srgb, var(--ui-primary) 10%, transparent); }
.connection-stage-main, .connection-stage-aside { position: relative; z-index: 1; }
.connection-stage-heading { display: flex; align-items: center; gap: 12px; }
.focus-device-status { display: inline-flex; align-items: center; gap: 6px; color: var(--ui-text-muted); font-size: 11px; }
.focus-device-status::before { width: 6px; height: 6px; border-radius: 50%; background: currentColor; content: ""; }
.focus-device-status.is-online { color: var(--ui-success); }
.focus-device-status.is-offline { color: var(--ui-danger); }
.connection-stage h2 { margin: 26px 0 4px; color: var(--ui-text); font-size: clamp(40px, 5.6vw, 76px); font-weight: 520; letter-spacing: -.045em; line-height: .98; }
.connection-stage-main > strong { color: var(--ui-primary); font-size: clamp(23px, 3vw, 36px); font-weight: 520; }
.connection-stage-main > p { margin: 14px 0 0; color: var(--ui-text-muted); font-size: 13px; }
.connection-path { position: relative; width: min(100%, 620px); margin-top: 62px; display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); }
.connection-path-line { position: absolute; top: 18px; right: 9%; left: 9%; height: 1px; background: linear-gradient(90deg, transparent, color-mix(in srgb, var(--ui-primary) 72%, transparent), transparent); }
.connection-path-line::after { position: absolute; top: -3px; left: 0; width: 7px; height: 7px; border-radius: 50%; background: var(--ui-primary); box-shadow: 0 0 14px var(--ui-primary); content: ""; animation: connection-signal 2.4s linear infinite; }
.connection-path-step { position: relative; z-index: 1; display: grid; place-items: center; gap: 9px; }
.connection-path-step span { width: 37px; height: 37px; display: grid; place-items: center; border: 1px solid var(--ui-border); border-radius: 50%; background: var(--ui-surface-strong); color: var(--ui-text-muted); }
.connection-path-step small { color: var(--ui-text-muted); font-size: 10px; }
.connection-path-step.is-ready span { border-color: var(--ui-primary); color: var(--ui-primary); box-shadow: 0 0 22px color-mix(in srgb, var(--ui-primary) 18%, transparent); }
.connection-path-step.is-failed span { border-color: var(--ui-danger); color: var(--ui-danger); }
.cellular-focus { width: min(100%, 520px); margin-top: 64px; padding-top: 24px; display: flex; align-items: end; justify-content: space-between; border-top: 1px solid var(--ui-border); color: var(--ui-text-muted); }
.cellular-focus > strong { color: var(--ui-text); font: 42px/1 "v-mono", monospace; }
.cellular-focus small { font-size: 13px; }
.connection-stage-aside { padding-left: 28px; display: flex; flex-direction: column; border-left: 1px solid var(--ui-border); }
.focus-stat { padding: 19px 0; display: grid; gap: 6px; border-bottom: 1px solid var(--ui-border); }
.focus-stat span { color: var(--ui-text-muted); font-size: 11px; }
.focus-stat strong { color: var(--ui-text); font-size: 20px; font-weight: 520; }
.focus-stat-ip strong { overflow: hidden; font: 14px "v-mono", monospace; text-overflow: ellipsis; white-space: nowrap; }
.focus-open-button { min-height: 44px; margin-top: auto; border: 1px solid color-mix(in srgb, var(--ui-primary) 52%, var(--ui-border)); border-radius: 12px; background: color-mix(in srgb, var(--ui-primary) 9%, transparent); color: var(--ui-primary); cursor: pointer; }
.fleet-summary { margin-bottom: 28px; padding: 16px 22px; display: flex; align-items: center; justify-content: space-between; gap: 30px; border: 1px solid var(--ui-border); border-radius: 16px; background: var(--ui-surface); }
.fleet-summary-copy { display: flex; align-items: baseline; gap: 10px; white-space: nowrap; }
.fleet-summary-copy > strong { color: var(--ui-text); font: 24px "v-mono", monospace; }
.fleet-summary-copy > span:last-child { color: var(--ui-text-muted); font-size: 11px; }
.fleet-metrics { display: grid; grid-template-columns: repeat(4, minmax(100px, 1fr)); }
.fleet-metric { min-width: 110px; padding: 0 20px; display: grid; grid-template-columns: auto 1fr; align-items: center; gap: 2px 9px; border-left: 1px solid var(--ui-border); }
.fleet-metric .el-icon { grid-row: span 2; color: var(--ui-primary); }
.fleet-metric span { color: var(--ui-text-muted); font-size: 10px; }
.fleet-metric strong { color: var(--ui-text); font: 16px "v-mono", monospace; }
.device-overview-toolbar { margin: 2px 0 16px; display: flex; align-items: end; justify-content: space-between; gap: 24px; }
.device-overview-toolbar h2 { margin: 4px 0 3px; color: var(--ui-text); font-size: 22px; font-weight: 620; }
.device-overview-toolbar p { margin: 0; color: var(--ui-text-muted); font-size: 13px; }
.device-filter-controls { width: min(100%, 560px); display: grid; grid-template-columns: minmax(220px, 1fr) auto; gap: 10px; }
.device-status-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 286px), 1fr)); gap: 12px; }
.device-status-grid :deep(.device-card) { animation: device-card-enter 240ms var(--ui-ease-out) both; animation-delay: min(calc(var(--device-index, 0) * 35ms), 210ms); }
.focus-swap-enter-active { transition: opacity 220ms var(--ui-ease-out), transform 220ms var(--ui-ease-out); }
.focus-swap-leave-active { transition: opacity 120ms var(--ui-ease-out), transform 120ms var(--ui-ease-out); }
.focus-swap-enter-from { opacity: 0; transform: translateY(10px) scale(.992); }
.focus-swap-leave-to { opacity: 0; transform: translateY(-4px) scale(.996); }
@keyframes connection-signal { from { opacity: 0; transform: translateX(0); } 12% { opacity: 1; } 88% { opacity: 1; } to { opacity: 0; transform: translateX(min(510px, 50vw)); } }
@keyframes device-card-enter { from { opacity: 0; transform: translateY(8px) scale(.99); } to { opacity: 1; transform: translateY(0) scale(1); } }
@media (prefers-reduced-motion: reduce) { .connection-path-line::after { animation: none; opacity: .7; } .device-status-grid :deep(.device-card) { animation: device-card-fade 160ms ease both; } .focus-swap-enter-from, .focus-swap-leave-to { transform: none; } @keyframes device-card-fade { from { opacity: 0; } to { opacity: 1; } } }
@media (max-width: 1050px) { .connection-stage { grid-template-columns: minmax(0, 1fr) 240px; } .fleet-summary { align-items: stretch; flex-direction: column; } .fleet-metrics { width: 100%; } .fleet-metric:first-child { border-left: 0; } }
@media (max-width: 760px) { .connection-stage { min-height: 0; grid-template-columns: minmax(0, 1fr); } .connection-stage-aside { padding: 12px 0 0; border-top: 1px solid var(--ui-border); border-left: 0; } .focus-open-button { margin-top: 18px; } .fleet-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); } .fleet-metric:nth-child(3) { border-left: 0; } .fleet-metric { padding: 10px 12px; } .device-overview-toolbar { align-items: stretch; flex-direction: column; } .device-filter-controls { width: 100%; grid-template-columns: minmax(0, 1fr); } }
</style>
