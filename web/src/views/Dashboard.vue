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
      <ConnectionFocusStage
        v-if="selectedDevice"
        :key="selectedDevice.id"
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
.section-kicker { color: var(--ui-primary); font: 700 10px "v-mono", monospace; letter-spacing: .14em; }
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
@keyframes device-card-enter { from { opacity: 0; transform: translateY(8px) scale(.99); } to { opacity: 1; transform: translateY(0) scale(1); } }
@media (prefers-reduced-motion: reduce) { .device-status-grid :deep(.device-card) { animation: device-card-fade 160ms ease both; } .focus-swap-enter-from, .focus-swap-leave-to { transform: none; } @keyframes device-card-fade { from { opacity: 0; } to { opacity: 1; } } }
@media (max-width: 1050px) { .fleet-summary { align-items: stretch; flex-direction: column; } .fleet-metrics { width: 100%; } .fleet-metric:first-child { border-left: 0; } }
@media (max-width: 760px) { .fleet-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); } .fleet-metric:nth-child(3) { border-left: 0; } .fleet-metric { padding: 10px 12px; } .device-overview-toolbar { align-items: stretch; flex-direction: column; } .device-filter-controls { width: 100%; grid-template-columns: minmax(0, 1fr); } }
</style>
