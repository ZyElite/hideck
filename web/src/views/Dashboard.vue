<script setup lang="ts">
import { onMounted, ref, computed } from 'vue'
import { storeToRefs } from 'pinia'
import { useRouter } from 'vue-router'
import DeviceCard from '../components/DeviceCard.vue'
import ConnectionFocusStage from '../components/ConnectionFocusStage.vue'
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
import { filterDashboardDevices } from '../utils/dashboardPresentation'
import { Search } from '@element-plus/icons-vue'
import { Add24Regular, ArrowRight24Regular } from '@vicons/fluent'

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
const filteredDevices = computed(() => filterDashboardDevices(devices.value, {
  query: searchQuery.value,
  status: statusFilter.value
}))
const deviceGridKey = computed(() => [
  statusFilter.value,
  searchQuery.value.trim().toLocaleLowerCase(),
  filteredDevices.value.map(device => device.id).join(',')
].join(':'))
const selectedDevice = computed(() => {
  return devices.value.find((device) => device.id === selectedDeviceID.value)
    || devices.value.find((device) => device.healthy)
    || devices.value[0]
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

function openDeviceOverview(id?: string) {
  const deviceID = String(id || '').trim()
  if (!deviceID) {
    void router.push({ name: 'Devices' })
    return
  }
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
      <ConnectionFocusStage
        :key="selectedDevice?.id || 'empty'"
        :device="selectedDevice"
        @open="openDeviceOverview"
      />
    </Transition>

    <section class="fleet-summary" aria-label="设备状态摘要">
      <div class="fleet-summary-copy">
        <span class="section-kicker">FLEET SUMMARY</span>
        <strong>{{ onlineCount }} / {{ totalCount }}</strong>
        <span>台设备在线</span>
      </div>
      <div class="fleet-metrics">
        <div class="fleet-metric">
          <span>全部</span>
          <strong>{{ totalCount }}</strong>
        </div>
        <div class="fleet-metric">
          <span>在线</span>
          <strong>{{ onlineCount }}</strong>
        </div>
        <div class="fleet-metric">
          <span>离线</span>
          <strong>{{ offlineCount }}</strong>
        </div>
        <div class="fleet-metric fleet-metric-time">
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

    <section class="device-overview-toolbar" aria-label="设备筛选">
      <div>
        <span class="section-kicker">DEVICE FLEET</span>
        <h2>设备连接</h2>
        <p>显示 {{ filteredDevices.length }} / {{ totalCount }} 台设备</p>
      </div>
      <div v-if="devices.length > 0" class="device-filter-controls">
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

    <ListSkeleton v-if="loading && devices.length === 0" :rows="4" />

    <button
      v-else-if="devices.length === 0"
      type="button"
      class="device-fleet-empty"
      aria-label="打开设备管理，添加或接管设备"
      @click="openDeviceOverview()"
    >
      <span class="device-fleet-empty-icon" aria-hidden="true"><Add24Regular /></span>
      <span class="device-fleet-empty-copy">
        <strong>等待设备接入</strong>
        <small>添加或接管设备后，这里会显示实时连接状态</small>
      </span>
      <span class="device-fleet-empty-action">
        管理设备
        <ArrowRight24Regular aria-hidden="true" />
      </span>
    </button>

    <template v-else>
      <EmptyState
        v-if="filteredDevices.length === 0"
        title="没有匹配的设备"
        subtitle="请调整搜索关键词或在线状态筛选"
      />
      <section v-else :key="deviceGridKey" class="device-status-grid" aria-label="设备实时状态">
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
      class="dashboard-traffic"
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
.dashboard-page :deep(.page-header) { margin-bottom: 26px; }
.section-kicker { color: var(--ui-primary); font: 700 10px "v-mono", monospace; letter-spacing: .14em; }
.fleet-summary { min-height: 79px; margin-bottom: 30px; padding: 15px 22px; display: grid; grid-template-columns: auto minmax(0, 1fr); align-items: center; gap: 18px; border: 1px solid var(--ui-border); border-radius: 22px; background: var(--ui-surface); }
.fleet-summary-copy { display: flex; align-items: baseline; gap: 10px; white-space: nowrap; }
.fleet-summary-copy > strong { color: var(--ui-text); font-size: 26px; }
.fleet-summary-copy > span:last-child { color: var(--ui-text-muted); font-size: 11px; }
.fleet-metrics { justify-self: end; display: grid; grid-template-columns: repeat(4, minmax(92px, 120px)); }
.fleet-metric { min-width: 0; padding-left: 18px; display: grid; gap: 2px; border-left: 1px solid var(--ui-border); }
.fleet-metric span { color: var(--ui-text-muted); font-size: 10px; }
.fleet-metric strong { color: var(--ui-text); font: 17px "v-mono", monospace; }
.device-overview-toolbar { margin: 0 0 16px; display: flex; align-items: end; justify-content: space-between; gap: 24px; }
.device-overview-toolbar h2 { margin: 5px 0 2px; color: var(--ui-text); font-size: 24px; font-weight: 620; }
.device-overview-toolbar p { margin: 0; color: var(--ui-text-muted); font-size: 13px; }
.device-filter-controls { width: min(100%, 560px); display: grid; grid-template-columns: minmax(220px, 1fr) auto; gap: 10px; }
.device-status-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 290px), 1fr)); gap: 12px; }
.device-fleet-empty {
  width: 100%;
  min-height: 92px;
  padding: 18px 20px;
  display: grid;
  grid-template-columns: 44px minmax(0, 1fr) auto;
  align-items: center;
  gap: 16px;
  border: 1px solid var(--ui-border);
  border-radius: 14px;
  background: linear-gradient(110deg, var(--ui-surface) 0 68%, color-mix(in srgb, var(--ui-primary) 5%, var(--ui-surface)) 100%);
  color: var(--ui-text);
  text-align: left;
  cursor: pointer;
  transition: border-color 160ms var(--ui-ease-out), background-color 160ms var(--ui-ease-out), transform 140ms var(--ui-ease-out);
}
.device-fleet-empty-icon { width: 44px; height: 44px; display: grid; place-items: center; border: 1px solid var(--ui-border); border-radius: 11px; background: var(--ui-surface-strong); color: var(--ui-primary); }
.device-fleet-empty-icon svg { width: 21px; height: 21px; }
.device-fleet-empty-copy { min-width: 0; display: grid; gap: 4px; }
.device-fleet-empty-copy strong { font-size: 14px; font-weight: 620; }
.device-fleet-empty-copy small { color: var(--ui-text-muted); font-size: 12px; line-height: 1.45; }
.device-fleet-empty-action { min-height: 38px; padding: 0 13px; display: inline-flex; align-items: center; gap: 8px; border: 1px solid color-mix(in srgb, var(--ui-primary) 42%, var(--ui-border)); border-radius: 9px; color: var(--ui-primary); font-size: 12px; font-weight: 620; }
.device-fleet-empty-action svg { width: 17px; height: 17px; }
.device-fleet-empty:active { transform: scale(.995); }
.dashboard-traffic { margin-top: 18px !important; }
.device-status-grid :deep(.device-card) { animation: device-card-enter 240ms var(--ui-ease-out) both; animation-delay: min(calc(var(--device-index, 0) * 35ms), 210ms); }
.focus-swap-enter-active { transition: opacity 220ms var(--ui-ease-out), transform 220ms var(--ui-ease-out); }
.focus-swap-leave-active { transition: opacity 120ms var(--ui-ease-out), transform 120ms var(--ui-ease-out); }
.focus-swap-enter-from { opacity: 0; transform: translateY(10px) scale(.992); }
.focus-swap-leave-to { opacity: 0; transform: translateY(-4px) scale(.996); }
@keyframes device-card-enter { from { opacity: 0; transform: translateY(8px) scale(.99); } to { opacity: 1; transform: translateY(0) scale(1); } }
@media (hover: hover) and (pointer: fine) { .device-fleet-empty:hover { border-color: color-mix(in srgb, var(--ui-primary) 48%, var(--ui-border)); background: color-mix(in srgb, var(--ui-primary) 6%, var(--ui-surface)); } }
@media (prefers-reduced-motion: reduce) { .device-status-grid :deep(.device-card) { animation: device-card-fade 160ms ease both; } .focus-swap-enter-from, .focus-swap-leave-to, .device-fleet-empty:active { transform: none; } @keyframes device-card-fade { from { opacity: 0; } to { opacity: 1; } } }
@media (max-width: 1050px) { .fleet-summary { grid-template-columns: auto auto 1fr; } .fleet-metrics { display: none; } }
@media (max-width: 760px) { .fleet-summary { grid-template-columns: minmax(0, 1fr); border-radius: 12px; } .fleet-summary-copy { white-space: normal; } .device-overview-toolbar { align-items: stretch; flex-direction: column; } .device-filter-controls { width: 100%; grid-template-columns: minmax(0, 1fr); } .device-fleet-empty { min-height: 118px; padding: 16px; grid-template-columns: 40px minmax(0, 1fr); gap: 12px; } .device-fleet-empty-icon { width: 40px; height: 40px; } .device-fleet-empty-action { min-height: 44px; grid-column: 1 / -1; justify-content: space-between; } }
</style>
