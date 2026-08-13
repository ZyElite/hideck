<script setup lang="ts">
import { computed, ref, shallowRef, watchEffect } from 'vue'
import ErrorState from './ErrorState.vue'
import RefreshButton from './RefreshButton.vue'
import type { AppError } from '../types/domain'
import type { TrafficAnalysis, TrafficRange } from '../services/traffic'
import { formatDeviceDate, formatDeviceDateTime, formatDeviceMonthDay, formatDeviceTime } from '../utils/deviceTime'

type TrafficAnalysisMode = 'global' | 'device'
type TooltipParam = {
  axisValue?: string | number
  dataIndex?: number
  seriesName?: string
  value?: string | number
  color?: string
}

type EChartsCoreModule = {
  use: (components: unknown[]) => void
}

type EChartsRendererModule = {
  CanvasRenderer?: unknown
}

type EChartsChartsModule = {
  LineChart?: unknown
}

type EChartsComponentsModule = {
  GridComponent?: unknown
  TooltipComponent?: unknown
  LegendComponent?: unknown
  DataZoomComponent?: unknown
}

type VueEChartsModule = {
  default: unknown
}

const props = withDefaults(defineProps<{
  analysis: TrafficAnalysis
  loading?: boolean
  error?: AppError | null
  lastOkAt?: number | null
  range: TrafficRange
  mode: TrafficAnalysisMode
  title?: string
  subtitle?: string
  disabled?: boolean
  deviceLabel?: string
  compact?: boolean
}>(), {
  title: '流量分析',
  subtitle: '数据每分钟采样一次，按日/周/月聚合',
  disabled: false,
  compact: false
})

const emit = defineEmits<{
  (e: 'update:range', value: TrafficRange): void
  (e: 'refresh'): void
}>()

const VChartComp = shallowRef<unknown>(null)
const chartLoadError = ref<string | null>(null)
const chartLoading = ref(false)
const detailsVisible = ref(false)
let chartInitPromise: Promise<void> | null = null

function formatChartLoadError(err: unknown) {
  if (err instanceof Error) return `${err.name}: ${err.message}`
  if (typeof err === 'string') return err
  try {
    return JSON.stringify(err) || '图表模块加载失败'
  } catch {
    return '图表模块加载失败'
  }
}

// Vite dev 模式下依赖预打包重新优化会让旧的 ?v=hash 立即失效；若图表的懒加载 import()
// 恰好与那次重新优化撞车，会报这类错误。这是瞬时竞态，重试一次通常就能拿到新哈希成功。
// 生产构建（无依赖再优化）不会触发，因此这里不需要也不应该全局处理。
const CHUNK_LOAD_ERROR_PATTERN =
  /Loading chunk|ChunkLoadError|dynamically imported module|Importing a module script failed|Failed to fetch dynamically imported module/i

function isChunkLoadLikeError(err: unknown) {
  const msg = err instanceof Error ? err.message : typeof err === 'string' ? err : ''
  return CHUNK_LOAD_ERROR_PATTERN.test(msg)
}

async function ensureChartLoaded() {
  if (VChartComp.value) return

  const maxAttempts = 2
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    if (!chartInitPromise) {
      chartLoading.value = true
      chartLoadError.value = null
      chartInitPromise = (async () => {
        const [core, renderers, charts, comps, vueEcharts] = await Promise.all([
          import('echarts/core'),
          import('echarts/renderers'),
          import('echarts/charts'),
          import('echarts/components'),
          import('vue-echarts')
        ])
        const coreMod = core as unknown as EChartsCoreModule
        const rendererMod = renderers as unknown as EChartsRendererModule
        const chartMod = charts as unknown as EChartsChartsModule
        const compMod = comps as unknown as EChartsComponentsModule
        const vueEchartsMod = vueEcharts as unknown as VueEChartsModule
        coreMod.use([
          rendererMod.CanvasRenderer,
          chartMod.LineChart,
          compMod.GridComponent,
          compMod.TooltipComponent,
          compMod.LegendComponent,
          compMod.DataZoomComponent
        ])
        VChartComp.value = vueEchartsMod.default
      })()
    }

    try {
      await chartInitPromise
      chartLoading.value = false
      return
    } catch (err) {
      chartInitPromise = null
      if (attempt < maxAttempts && isChunkLoadLikeError(err)) {
        await new Promise((resolve) => setTimeout(resolve, 400))
        continue
      }
      chartLoadError.value = formatChartLoadError(err)
      chartLoading.value = false
      return
    }
  }
}

const analysisBuckets = computed(() => props.analysis.buckets || [])
const analysisChartData = computed(() => props.analysis.chart)

function formatBytes(bytes: unknown) {
  const v = Number(bytes) || 0
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let val = v
  let i = 0
  while (val >= 1024 && i < units.length - 1) {
    val /= 1024
    i++
  }
  return `${val.toFixed(i === 0 ? 0 : 2)} ${units[i]}`
}

function formatTrafficAxisTime(periodStart: unknown, fallback: string) {
  if (typeof periodStart !== 'string') return fallback
  if (props.range === 'day') return formatDeviceTime(periodStart, { fallback }).slice(0, 5)
  return formatDeviceMonthDay(periodStart, { fallback })
}

function formatTrafficTooltipTime(periodStart: unknown, fallback: string | number | undefined) {
  const fallbackText = String(fallback ?? '')
  if (typeof periodStart !== 'string') return fallbackText
  if (props.range === 'day') return formatDeviceDateTime(periodStart, { fallback: fallbackText }).slice(0, 16)
  return formatDeviceDate(periodStart, { fallback: fallbackText })
}

function formatTrafficBucketTime(bucket: { period_start?: string; bucket?: string }) {
  const fallback = bucket?.bucket || ''
  if (!bucket?.period_start) return fallback
  if (props.range === 'day') return formatDeviceDateTime(bucket.period_start, { fallback }).slice(0, 16)
  return formatDeviceDate(bucket.period_start, { fallback })
}

const analysisTotal = computed(() => {
  const rx = analysisBuckets.value.reduce((sum, bucket) => sum + (Number(bucket.rx_bytes) || 0), 0)
  const tx = analysisBuckets.value.reduce((sum, bucket) => sum + (Number(bucket.tx_bytes) || 0), 0)
  return { rx, tx, total: rx + tx }
})

const rangeText = computed(() => {
  const map: Record<TrafficRange, string> = {
    day: '本日',
    week: '本周',
    month: '本月'
  }
  return map[props.range] || '本周期'
})

function pickUnit(maxBytes: number) {
  const gb = 1024 * 1024 * 1024
  const mb = 1024 * 1024
  const kb = 1024
  if (maxBytes >= gb) return { label: 'GB', divisor: gb, decimals: 2 }
  if (maxBytes >= mb) return { label: 'MB', divisor: mb, decimals: 2 }
  if (maxBytes >= kb) return { label: 'KB', divisor: kb, decimals: 2 }
  return { label: 'B', divisor: 1, decimals: 0 }
}

const chartSeriesSnapshot = computed(() => {
  const chart = analysisChartData.value
  if (!chart) return null

  const timestamps = Array.isArray(chart.timestamps) ? chart.timestamps : []
  const periodStarts = Array.isArray(chart.period_starts) ? chart.period_starts : []
  const displayTimestamps = timestamps.map((label, idx) =>
    formatTrafficAxisTime(periodStarts[idx], String(label || ''))
  )

  const totalBytesByTs = timestamps.map((_, idx) =>
    chart.devices.reduce((sum, dev) => sum + Number(chart.series[dev]?.[idx] || 0), 0)
  )

  return {
    timestamps: displayTimestamps,
    periodStarts,
    devices: chart.devices,
    series: chart.series,
    totalBytesByTs
  }
})

const hasChartData = computed(() => {
  const snapshot = chartSeriesSnapshot.value
  if (!snapshot) return false
  if (snapshot.timestamps.length === 0) return false
  if (props.mode === 'device') {
    return snapshot.totalBytesByTs.some(v => v > 0)
  }
  return snapshot.devices.length > 0 && snapshot.totalBytesByTs.some(v => v > 0)
})

const deviceSeriesName = computed(() => {
  const label = String(props.deviceLabel || '').trim()
  if (label) return label
  return analysisChartData.value?.devices?.[0] || '当前设备'
})

const panelClass = computed(() => [
  'traffic-analysis-panel overflow-hidden',
  props.mode === 'device' ? 'ui-panel-muted' : 'ui-card is-global',
  props.compact ? 'is-compact' : ''
])

const chartOption = computed(() => {
  const snapshot = chartSeriesSnapshot.value
  if (!snapshot || !hasChartData.value) return null

  const { timestamps, devices, series, totalBytesByTs } = snapshot
  const maxBytes = Math.max(0, ...totalBytesByTs)
  const unit = pickUnit(maxBytes)

  if (props.compact && props.mode === 'global') {
    const rxBytes = analysisBuckets.value.map((bucket) => Number(bucket.rx_bytes) || 0)
    const txBytes = analysisBuckets.value.map((bucket) => Number(bucket.tx_bytes) || 0)
    const compactMaxBytes = Math.max(0, ...rxBytes, ...txBytes)
    const compactUnit = pickUnit(compactMaxBytes)
    const compactTimestamps = analysisBuckets.value.map((bucket) => formatTrafficBucketTime(bucket))
    return buildCompactChartOption(compactTimestamps, rxBytes, txBytes, compactUnit)
  }

  if (props.mode === 'device') {
    return {
      tooltip: {
        trigger: 'axis',
        axisPointer: { type: 'cross', label: { backgroundColor: '#6a7985' } },
        formatter: (params: unknown) => {
          const list: TooltipParam[] = Array.isArray(params)
            ? params.filter((item): item is TooltipParam => !!item && typeof item === 'object')
            : []
          const axisLabel = list[0]?.axisValue ?? ''
          const dataIndex = Number(list[0]?.dataIndex ?? -1)
          const timeLabel = formatTrafficTooltipTime(snapshot.periodStarts[dataIndex], axisLabel)
          const point = list[0]
          const value = Number(point?.value) || 0
          return `<div class="font-bold mb-1">${timeLabel}</div>
            <div class="flex justify-between gap-4 text-xs">
              <span>${deviceSeriesName.value}</span>
              <span class="font-mono">${value.toFixed(unit.decimals)} ${unit.label}</span>
            </div>`
        }
      },
      grid: {
        left: '3%',
        right: '4%',
        bottom: 28,
        top: 24,
        containLabel: true
      },
      xAxis: [
        {
          type: 'category',
          boundaryGap: false,
          data: timestamps,
          axisLine: { lineStyle: { color: '#4b5563' } }
        }
      ],
      yAxis: [
        {
          type: 'value',
          name: `流量 (${unit.label})`,
          splitLine: { lineStyle: { color: '#374151', type: 'dashed', opacity: 0.3 } },
          axisLine: { lineStyle: { color: '#4b5563' } }
        }
      ],
      dataZoom: [
        {
          type: 'inside',
          filterMode: 'none'
        }
      ],
      series: [
        {
          name: deviceSeriesName.value,
          type: 'line',
          symbol: 'none',
          smooth: true,
          areaStyle: { opacity: 0.18 },
          lineStyle: { width: 2.5, opacity: 0.95 },
          emphasis: { focus: 'series' },
          data: totalBytesByTs.map(v => v / unit.divisor)
        }
      ],
      backgroundColor: 'transparent'
    }
  }

  const stackedSeries = devices.map(dev => ({
    name: dev,
    type: 'line',
    stack: 'Total',
    areaStyle: {},
    symbol: 'none',
    smooth: true,
    emphasis: { focus: 'series' },
    data: (series[dev] || []).map(v => Number(v || 0) / unit.divisor)
  }))

  const totalSeries = {
    name: '总流量',
    type: 'line',
    symbol: 'none',
    smooth: true,
    lineStyle: { width: 2, opacity: 0.9 },
    emphasis: { focus: 'series' },
    data: totalBytesByTs.map(v => v / unit.divisor)
  }

  return {
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'cross', label: { backgroundColor: '#6a7985' } },
      formatter: (params: unknown) => {
        const list: TooltipParam[] = Array.isArray(params)
          ? params.filter((item): item is TooltipParam => !!item && typeof item === 'object')
          : []
        const axisLabel = list[0]?.axisValue ?? ''
        const dataIndex = Number(list[0]?.dataIndex ?? -1)
        const timeLabel = formatTrafficTooltipTime(snapshot.periodStarts[dataIndex], axisLabel)
        const totalItem = list.find(item => item?.seriesName === '总流量')
        const deviceItems = list.filter(item => item?.seriesName !== '总流量')
        deviceItems.sort((a, b) => (Number(b?.value) || 0) - (Number(a?.value) || 0))

        const topItems = deviceItems.slice(0, 6)
        const otherItems = deviceItems.slice(6)
        const otherSum = otherItems.reduce((sum, item) => sum + (Number(item?.value) || 0), 0)

        let res = `<div class="font-bold mb-1">${timeLabel}</div>`
        const totalVal = Number(totalItem?.value) || 0
        res += `<div class="flex justify-between gap-4 text-xs font-bold">
          <span>总流量</span>
          <span class="font-mono">${totalVal.toFixed(unit.decimals)} ${unit.label}</span>
        </div>`
        res += `<div class="mt-1">`
        topItems.forEach(item => {
          const val = Number(item?.value) || 0
          res += `<div class="flex justify-between gap-4 text-xs">
            <span style="color:${item.color}">● ${item.seriesName}</span>
            <span class="font-mono">${val.toFixed(unit.decimals)} ${unit.label}</span>
          </div>`
        })
        if (otherItems.length > 0) {
          res += `<div class="flex justify-between gap-4 text-xs text-gray-500">
            <span>其他（${otherItems.length}）</span>
            <span class="font-mono">${otherSum.toFixed(unit.decimals)} ${unit.label}</span>
          </div>`
        }
        res += `</div>`
        return res
      }
    },
    legend: {
      type: 'scroll',
      data: ['总流量', ...devices],
      textStyle: { color: '#9ca3af' },
      top: 0,
      left: 10,
      right: 10,
      height: 44
    },
    grid: {
      left: '3%',
      right: '4%',
      bottom: 28,
      top: 56,
      containLabel: true
    },
    xAxis: [
      {
        type: 'category',
        boundaryGap: false,
        data: timestamps,
        axisLine: { lineStyle: { color: '#4b5563' } }
      }
    ],
    yAxis: [
      {
        type: 'value',
        name: `流量 (${unit.label})`,
        splitLine: { lineStyle: { color: '#374151', type: 'dashed', opacity: 0.3 } },
        axisLine: { lineStyle: { color: '#4b5563' } }
      }
    ],
    dataZoom: [
      {
        type: 'inside',
        filterMode: 'none'
      }
    ],
    series: [totalSeries, ...stackedSeries],
    backgroundColor: 'transparent'
  }
})

type TrafficUnit = ReturnType<typeof pickUnit>

function buildCompactChartOption(timestamps: string[], rxBytes: number[], txBytes: number[], unit: TrafficUnit) {
  const line = (name: string, color: string, values: number[]) => ({
    name,
    type: 'line',
    symbol: 'none',
    smooth: true,
    lineStyle: { width: 2, color },
    emphasis: { focus: 'series' },
    data: values.map((value) => value / unit.divisor)
  })
  return {
    color: ['#246bce', '#198754'],
    tooltip: { trigger: 'axis' },
    legend: { data: ['下载', '上传'], top: 0, right: 8 },
    grid: { left: 12, right: 18, top: 38, bottom: 22, containLabel: true },
    xAxis: [{
      type: 'category',
      boundaryGap: false,
      data: timestamps,
      axisTick: { show: false },
      axisLine: { lineStyle: { color: '#9aabad' } }
    }],
    yAxis: [{
      type: 'value',
      name: `流量 (${unit.label})`,
      splitLine: { lineStyle: { color: '#9aabad', type: 'dashed', opacity: 0.28 } }
    }],
    dataZoom: [{ type: 'inside', filterMode: 'none' }],
    series: [line('下载', '#246bce', rxBytes), line('上传', '#198754', txBytes)],
    backgroundColor: 'transparent'
  }
}

watchEffect(() => {
  if (chartOption.value && !VChartComp.value && !chartLoadError.value) {
    void ensureChartLoaded()
  }
})

function retryChartLoad() {
  chartLoadError.value = null
  chartInitPromise = null
  void ensureChartLoaded()
}

function handleRangeChange(value: string | number | boolean | undefined) {
  if (value === 'day' || value === 'week' || value === 'month') {
    emit('update:range', value)
  }
}
</script>

<template>
  <div :class="panelClass">
    <div class="traffic-panel-header">
      <div class="traffic-panel-heading">
        <span v-if="mode === 'global'" class="traffic-panel-kicker">TRAFFIC ANALYSIS</span>
        <h2>{{ title }}</h2>
        <p>{{ subtitle }}</p>
      </div>
      <div class="traffic-panel-actions">
        <el-radio-group
          class="traffic-period-control"
          :model-value="range"
          :disabled="disabled"
          aria-label="流量统计周期"
          @change="handleRangeChange"
        >
          <el-radio-button value="day">日</el-radio-button>
          <el-radio-button value="week">周</el-radio-button>
          <el-radio-button value="month">月</el-radio-button>
        </el-radio-group>
        <RefreshButton :loading="loading" :disabled="disabled" @click="emit('refresh')" />
        <el-button
          v-if="!compact"
          :aria-expanded="detailsVisible"
          @click="detailsVisible = !detailsVisible"
        >
          {{ detailsVisible ? '收起明细' : '查看明细' }}
        </el-button>
      </div>
    </div>

    <div v-if="disabled" class="ui-panel-muted p-6 text-sm text-gray-400 dark:text-gray-500">
      网络已禁用，暂无流量分析
    </div>

    <template v-else>
      <ErrorState
        v-if="error"
        class="mb-4"
        title="流量分析加载失败"
        :message="error.message"
        :status-code="error.status"
        :request-method="error.method"
        :request-url="error.url"
        :last-success-at="lastOkAt"
        retry-text="重试"
        @retry="emit('refresh')"
      />

      <div v-if="!compact" class="traffic-metrics" aria-label="周期流量摘要">
        <div class="traffic-metric is-download">
          <span>{{ rangeText }}下载</span>
          <strong>{{ formatBytes(analysisTotal.rx) }}</strong>
        </div>
        <div class="traffic-metric is-upload">
          <span>{{ rangeText }}上传</span>
          <strong>{{ formatBytes(analysisTotal.tx) }}</strong>
        </div>
        <div class="traffic-metric is-total">
          <span>{{ rangeText }}合计</span>
          <strong>{{ formatBytes(analysisTotal.total) }}</strong>
        </div>
      </div>

      <div v-if="chartOption && VChartComp" :class="compact ? 'compact-chart' : 'traffic-chart'">
        <component :is="VChartComp" class="chart" :option="chartOption" autoresize />
      </div>
      <ErrorState
        v-else-if="chartOption && chartLoadError"
        class="mb-6"
        title="流量图表加载失败"
        :message="chartLoadError"
        retry-text="重试图表"
        @retry="retryChartLoad"
      />
      <div
        v-else-if="chartOption && chartLoading"
        class="traffic-chart-placeholder ui-panel-muted border border-dashed border-gray-200 dark:border-white/10"
      >
        流量图表加载中...
      </div>
      <div
        v-else
        class="traffic-chart-placeholder ui-panel-muted border border-dashed border-gray-200 dark:border-white/10"
      >
        暂无流量图表数据
      </div>

      <footer v-if="compact" class="traffic-summary">
        <span>下载 <strong class="download-value">{{ formatBytes(analysisTotal.rx) }}</strong></span>
        <span>上传 <strong class="upload-value">{{ formatBytes(analysisTotal.tx) }}</strong></span>
        <span>总流量 <strong>{{ formatBytes(analysisTotal.total) }}</strong></span>
      </footer>

      <section v-else-if="detailsVisible" class="traffic-detail" aria-label="流量周期明细">
        <header>
          <div>
            <strong>周期明细</strong>
            <span>来自真实采样数据</span>
          </div>
          <span>{{ analysisBuckets.length }} 条记录</span>
        </header>
        <el-table
          :data="analysisBuckets"
          size="small"
          stripe
          v-loading="!!loading"
          class="traffic-detail-table"
        >
          <el-table-column label="时间" min-width="140">
            <template #default="scope">{{ formatTrafficBucketTime(scope?.row || {}) }}</template>
          </el-table-column>
          <el-table-column label="下载" min-width="120">
            <template #default="scope">{{ formatBytes(scope?.row?.rx_bytes) }}</template>
          </el-table-column>
          <el-table-column label="上传" min-width="120">
            <template #default="scope">{{ formatBytes(scope?.row?.tx_bytes) }}</template>
          </el-table-column>
          <el-table-column label="合计" min-width="120">
            <template #default="scope">{{ formatBytes(scope?.row?.total_bytes) }}</template>
          </el-table-column>
        </el-table>
      </section>
    </template>
  </div>
</template>

<style scoped>
.traffic-panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.traffic-analysis-panel {
  padding: 30px;
  border-radius: 22px;
  background: linear-gradient(155deg, color-mix(in srgb, var(--ui-surface) 98%, var(--ui-primary) 2%), var(--ui-surface));
}

.traffic-panel-heading { min-width: 0; }
.traffic-panel-kicker { display: block; margin-bottom: 6px; color: var(--ui-primary); font: 700 10px "v-mono", monospace; letter-spacing: .14em; }
.traffic-panel-heading h2 { margin: 0; color: var(--ui-text); font-size: 22px; font-weight: 650; line-height: 1.25; }
.traffic-panel-heading p { margin: 4px 0 0; color: var(--ui-text-muted); font-size: 13px; }

.traffic-metrics {
  margin: 26px 0 18px;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  border-block: 1px solid var(--ui-border);
}

.traffic-metric { min-width: 0; padding: 18px 20px; }
.traffic-metric + .traffic-metric { border-left: 1px solid var(--ui-border); }
.traffic-metric span { display: block; color: var(--ui-text-muted); font-size: 12px; }
.traffic-metric strong { display: block; margin-top: 5px; color: var(--ui-text); font: 700 20px "v-mono", ui-monospace, monospace; overflow-wrap: anywhere; }
.traffic-metric.is-download strong { color: var(--ui-communication); }
.traffic-metric.is-upload strong { color: var(--ui-success); }
.traffic-metric.is-total strong { color: var(--ui-primary); }

.traffic-chart {
  width: 100%;
  height: 235px;
}

.traffic-panel-actions {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: 8px;
}

.traffic-period-control { flex: 0 0 auto; }
.traffic-period-control :deep(.el-radio-button__inner) { min-width: 48px; min-height: 38px; display: grid; place-items: center; }

.compact-chart {
  width: 100%;
  height: 260px;
}

.traffic-chart-placeholder {
  height: 235px;
  margin-bottom: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 14px;
  color: var(--ui-text-muted);
  font-size: 13px;
}

.traffic-detail { min-width: 0; overflow: hidden; border-top: 1px solid var(--ui-border); }
.traffic-detail > header { min-height: 52px; display: flex; align-items: center; justify-content: space-between; gap: 16px; color: var(--ui-text-muted); font-size: 12px; }
.traffic-detail > header > div { min-width: 0; display: flex; align-items: baseline; gap: 9px; }
.traffic-detail > header strong { color: var(--ui-text); font-size: 13px; }
.traffic-detail-table { width: 100%; min-width: 0; }

.traffic-summary {
  min-height: 42px;
  margin: 0 -24px -24px;
  padding: 0 24px;
  display: flex;
  align-items: center;
  gap: 32px;
  border-top: 1px solid var(--ui-border);
  color: var(--ui-text-muted);
  font-size: 12px;
}

.traffic-summary strong {
  margin-left: 6px;
  color: var(--ui-text);
  font-family: "v-mono", ui-monospace, monospace;
}

.traffic-summary .download-value { color: var(--ui-communication); }
.traffic-summary .upload-value { color: var(--ui-success); }

@media (max-width: 639px) {
  .traffic-analysis-panel { padding: 20px; }

  .traffic-panel-header {
    align-items: stretch;
    flex-direction: column;
  }

  .traffic-panel-actions {
    justify-content: space-between;
  }

  .traffic-period-control :deep(.el-radio-button__inner) { min-width: 46px; min-height: 44px; padding-inline: 12px; }

  .traffic-metrics { margin-top: 22px; grid-template-columns: 1fr; }
  .traffic-metric { padding: 14px 4px; }
  .traffic-metric + .traffic-metric { border-left: 0; border-top: 1px solid var(--ui-border); }

  .traffic-chart { height: 220px; }

  .traffic-detail > header > div { align-items: flex-start; flex-direction: column; gap: 1px; }

  .compact-chart {
    height: 220px;
  }

  .traffic-summary {
    padding: 10px 24px;
    align-items: flex-start;
    flex-direction: column;
    gap: 5px;
  }
}
</style>
