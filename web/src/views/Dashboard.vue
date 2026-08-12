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

    <section class="dashboard-hero" aria-label="设备状态摘要">
      <div class="dashboard-hero-copy">
        <span class="dashboard-eyebrow">LIVE CONNECTIVITY</span>
        <h2>所有通信设备</h2>
        <p>状态数据来自实时设备探测，不使用静态占位数据。</p>
      </div>
      <div class="dashboard-metrics">
      <div class="metric-tile ui-panel">
        <div>
          <div class="metric-label">设备总数</div>
          <div class="metric-value">{{ totalCount }}</div>
        </div>
        <span class="metric-icon metric-icon-primary" aria-hidden="true">
          <el-icon><Server24Regular /></el-icon>
        </span>
      </div>
      <div class="metric-tile ui-panel">
        <div>
          <div class="metric-label">在线</div>
          <div class="metric-value metric-value-communication">{{ onlineCount }}</div>
        </div>
        <span class="metric-icon metric-icon-communication" aria-hidden="true">
          <el-icon><CheckmarkCircle24Regular /></el-icon>
        </span>
      </div>
      <div class="metric-tile ui-panel">
        <div>
          <div class="metric-label">离线</div>
          <div class="metric-value metric-value-danger">{{ offlineCount }}</div>
        </div>
        <span class="metric-icon metric-icon-danger" aria-hidden="true">
          <el-icon><DismissCircle24Regular /></el-icon>
        </span>
      </div>
      <div class="metric-tile ui-panel">
        <div>
          <div class="metric-label">最近刷新</div>
          <div class="metric-time">
            {{ devicesLastOkAt ? formatDeviceTime(devicesLastOkAt, { clientClock: true }) : '--:--:--' }}
          </div>
        </div>
        <span class="metric-icon metric-icon-primary" aria-hidden="true">
          <el-icon><Clock24Regular /></el-icon>
        </span>
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
          v-for="dev in filteredDevices"
          :key="dev.id"
          :device="dev"
          @open-device="openDeviceOverview"
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
.dashboard-hero {
  position: relative;
  margin-bottom: 26px;
  padding: 28px;
  display: grid;
  grid-template-columns: minmax(260px, 0.7fr) minmax(0, 1.3fr);
  gap: 28px;
  overflow: hidden;
  border: 1px solid var(--ui-border);
  border-radius: var(--ui-radius-xl);
  background:
    radial-gradient(circle at 38% 50%, color-mix(in srgb, var(--ui-primary) 10%, transparent), transparent 34%),
    var(--ui-surface);
}

.dashboard-hero::after {
  position: absolute;
  inset: 0 0 0 42%;
  opacity: 0.42;
  background-image: radial-gradient(circle, color-mix(in srgb, var(--ui-primary) 36%, transparent) 1px, transparent 1.4px);
  background-size: 18px 18px;
  mask-image: radial-gradient(ellipse at center, #000 0%, transparent 70%);
  pointer-events: none;
  content: "";
}

.dashboard-hero-copy,
.dashboard-metrics { position: relative; z-index: 1; }

.dashboard-eyebrow,
.section-kicker {
  color: var(--ui-primary);
  font-family: "v-mono", ui-monospace, monospace;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.14em;
}

.dashboard-hero-copy h2 {
  margin: 16px 0 8px;
  color: var(--ui-text);
  font-size: clamp(30px, 4vw, 48px);
  font-weight: 580;
  line-height: 1.02;
}

.dashboard-hero-copy p,
.device-overview-toolbar p {
  margin: 0;
  color: var(--ui-text-muted);
  font-size: 13px;
}

.metric-tile {
  min-width: 0;
  min-height: 104px;
  padding: 16px 18px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.dashboard-metrics,
.device-status-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.dashboard-metrics {
  align-self: stretch;
}

.metric-label {
  color: var(--ui-text-muted);
  font-size: 12px;
  font-weight: 650;
}

.metric-value {
  margin-top: 8px;
  color: var(--ui-text);
  font-family: "v-mono", ui-monospace, monospace;
  font-size: 30px;
  font-weight: 700;
  line-height: 1;
  font-variant-numeric: tabular-nums;
}

.metric-value-communication { color: var(--ui-communication); }
.metric-value-danger { color: var(--ui-danger); }

.metric-time {
  margin-top: 10px;
  color: var(--ui-text);
  font-family: "v-mono", ui-monospace, monospace;
  font-size: 18px;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
}

.metric-icon {
  width: 44px;
  height: 44px;
  flex: 0 0 44px;
  display: grid;
  place-items: center;
  border: 2px solid currentColor;
  border-radius: 50%;
  font-size: 24px;
}

.device-overview-toolbar {
  margin: 2px 0 16px;
  display: flex;
  align-items: end;
  justify-content: space-between;
  gap: 24px;
}

.device-overview-toolbar h2 {
  margin: 4px 0 3px;
  color: var(--ui-text);
  font-size: 22px;
  font-weight: 620;
}

.device-filter-controls {
  width: min(100%, 560px);
  display: grid;
  grid-template-columns: minmax(220px, 1fr) auto;
  gap: 10px;
}

.device-status-grid {
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 286px), 1fr));
}

.metric-icon-primary { color: var(--ui-primary); }
.metric-icon-communication { color: var(--ui-communication); }
.metric-icon-danger { color: var(--ui-danger); }

@media (max-width: 1199px) {
  .dashboard-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 899px) {
  .dashboard-hero { grid-template-columns: minmax(0, 1fr); }
  .device-overview-toolbar { align-items: stretch; flex-direction: column; }
  .device-filter-controls { width: 100%; }
}

@media (max-width: 639px) {
  .dashboard-hero { padding: 22px 18px; }
  .dashboard-metrics {
    grid-template-columns: minmax(0, 1fr);
  }

  .device-filter-controls { grid-template-columns: minmax(0, 1fr); }

  .metric-tile {
    min-height: 88px;
  }
}
</style>
