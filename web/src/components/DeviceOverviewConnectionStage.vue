<script setup lang="ts">
import { computed } from 'vue'
import {
  Checkmark12Regular,
  Dismiss12Regular,
  Subtract12Regular,
  Wifi124Regular
} from '@vicons/fluent'
import type { DeviceOverviewItem } from '../types/api'
import {
  createDashboardStages,
  formatDashboardSignal,
  hasDashboardSignal
} from '../utils/dashboardPresentation'

const props = defineProps<{
  device: DeviceOverviewItem | null
}>()

const stages = computed(() => createDashboardStages(props.device?.vowifi_runtime))
const hasFailedStage = computed(() => stages.value.some(stage => stage.ready === false))
const hasReadyStage = computed(() => stages.value.some(stage => stage.ready === true))
const allStagesReady = computed(() => stages.value.every(stage => stage.ready === true))

const serviceState = computed(() => {
  const device = props.device
  if (!device?.vowifi_enabled) {
    return { tone: 'is-idle', title: 'VoWiFi 未启用', detail: '当前设备使用蜂窝网络' }
  }
  if (hasFailedStage.value) {
    return { tone: 'is-failed', title: 'VoWiFi 链路异常', detail: runtimeReason.value || '请检查失败阶段' }
  }
  if (device.vowifi_active && allStagesReady.value) {
    return { tone: 'is-ready', title: 'VoWiFi 已连接', detail: '通过 Wi-Fi 建立安全隧道并注册 IMS' }
  }
  if (hasReadyStage.value) {
    return { tone: 'is-pending', title: 'VoWiFi 正在建立', detail: runtimeReason.value || '等待剩余阶段就绪' }
  }
  return { tone: 'is-idle', title: 'VoWiFi 等待连接', detail: runtimeReason.value || '尚未收到链路状态' }
})

const runtimeReason = computed(() => {
  const runtime = props.device?.vowifi_runtime
  return runtime?.sms_ready_reason || runtime?.last_reason || ''
})

const pathIsFlowing = computed(() => {
  return props.device?.healthy === true
    && props.device.vowifi_active === true
    && !hasFailedStage.value
})

const metrics = computed(() => [
  {
    label: '信号',
    value: formatDashboardSignal(props.device?.modem?.signal_dbm),
    hint: hasDashboardSignal(props.device?.modem?.signal_dbm)
      ? signalQuality(props.device.modem.signal_dbm)
      : ''
  },
  { label: '公网 IPv6', value: props.device?.public_ipv6 || '未分配' },
  { label: '协议', value: props.device?.backend_mode?.toUpperCase() || '不可用' },
  { label: '接口', value: props.device?.interface || '不可用' }
])

function signalQuality(value: number): string {
  if (value >= -75) return '优秀'
  if (value >= -90) return '良好'
  if (value >= -105) return '一般'
  return '较弱'
}

function stageLabel(ready: boolean | undefined): string {
  if (ready === true) return '已就绪'
  if (ready === false) return '失败'
  return '等待状态'
}
</script>

<template>
  <section class="overview-connection-stage" :class="serviceState.tone" aria-label="VoWiFi 连接状态">
    <div class="overview-connection-main">
      <span class="overview-eyebrow">WI-FI CALLING</span>
      <h2>
        <el-icon aria-hidden="true"><Wifi124Regular /></el-icon>
        {{ serviceState.title }}
      </h2>
      <p>{{ serviceState.detail }}</p>

      <div class="overview-service-path" :class="{ 'is-flowing': pathIsFlowing }" aria-label="VoWiFi 服务链路">
        <div class="overview-service-track" aria-hidden="true"><span /></div>
        <div
          v-for="stage in stages"
          :key="stage.key"
          class="overview-service-step"
          :class="{ 'is-ready': stage.ready === true, 'is-failed': stage.ready === false }"
          :aria-label="`${stage.key}：${stageLabel(stage.ready)}`"
        >
          <i aria-hidden="true">
            <Checkmark12Regular v-if="stage.ready === true" />
            <Dismiss12Regular v-else-if="stage.ready === false" />
            <Subtract12Regular v-else />
          </i>
          <small>{{ stage.key }}</small>
        </div>
      </div>
    </div>

    <dl class="overview-connection-metrics">
      <div v-for="metric in metrics" :key="metric.label">
        <dt>{{ metric.label }}</dt>
        <dd :title="metric.value">{{ metric.value }}</dd>
        <small v-if="metric.hint">{{ metric.hint }}</small>
      </div>
    </dl>
  </section>
</template>

<style scoped>
.overview-connection-stage {
  min-height: 268px;
  padding: 26px;
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(250px, 36%);
  gap: 26px;
  overflow: hidden;
  border: 1px solid var(--ui-border);
  border-radius: 16px;
  background:
    radial-gradient(circle at 72% 35%, color-mix(in srgb, var(--ui-primary) 10%, transparent), transparent 34%),
    var(--ui-surface-strong);
}

.overview-connection-main {
  min-width: 0;
}

.overview-eyebrow {
  color: var(--ui-primary);
  font: 700 9px "v-mono", ui-monospace, monospace;
  letter-spacing: .16em;
}

.overview-connection-stage h2 {
  margin: 12px 0 0;
  display: flex;
  align-items: center;
  gap: 10px;
  color: var(--ui-text);
  font-size: clamp(24px, 3vw, 34px);
  font-weight: 600;
  letter-spacing: -.025em;
}

.overview-connection-stage h2 .el-icon {
  color: var(--ui-primary);
}

.overview-connection-stage > div > p {
  margin: 6px 0 0;
  color: var(--ui-text-muted);
  font-size: 12px;
}

.overview-connection-stage.is-failed h2,
.overview-connection-stage.is-failed h2 .el-icon { color: var(--ui-danger); }
.overview-connection-stage.is-pending h2,
.overview-connection-stage.is-pending h2 .el-icon { color: var(--ui-warning); }

.overview-service-path {
  position: relative;
  max-width: 620px;
  margin-top: 48px;
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  container-type: inline-size;
}

.overview-service-track {
  position: absolute;
  top: 17px;
  right: 9%;
  left: 9%;
  height: 1px;
  background: var(--ui-border);
}

.overview-service-track span {
  position: absolute;
  top: -3px;
  left: 0;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  opacity: 0;
  background: var(--ui-primary);
  box-shadow: 0 0 12px var(--ui-primary);
}

.overview-service-path.is-flowing .overview-service-track span {
  animation: overview-service-flow 2.4s linear infinite;
}

.overview-service-step {
  position: relative;
  z-index: 1;
  min-width: 0;
  display: grid;
  place-items: center;
  gap: 7px;
  color: var(--ui-text-muted);
}

.overview-service-step i {
  width: 36px;
  height: 36px;
  display: grid;
  place-items: center;
  border: 1px solid var(--ui-border);
  border-radius: 50%;
  background: var(--ui-surface);
}

.overview-service-step svg { width: 13px; height: 13px; }
.overview-service-step small { font-size: 10px; }
.overview-service-step.is-ready { color: var(--ui-primary); }
.overview-service-step.is-ready i { border-color: var(--ui-primary); }
.overview-service-step.is-failed { color: var(--ui-danger); }
.overview-service-step.is-failed i { border-color: var(--ui-danger); }

.overview-connection-metrics {
  margin: 0;
  padding: 4px 0;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  border: 1px solid var(--ui-border);
  border-radius: 12px;
  background: color-mix(in srgb, var(--ui-surface) 82%, transparent);
}

.overview-connection-metrics div {
  min-width: 0;
  padding: 14px 16px;
}

.overview-connection-metrics div:nth-child(odd) { border-right: 1px solid var(--ui-border); }
.overview-connection-metrics div:nth-child(-n+2) { border-bottom: 1px solid var(--ui-border); }
.overview-connection-metrics dt { color: var(--ui-text-muted); font-size: 10px; }
.overview-connection-metrics dd { margin: 6px 0 0; color: var(--ui-text); font: 13px "v-mono", ui-monospace, monospace; overflow-wrap: anywhere; }
.overview-connection-metrics small { color: var(--ui-primary); font-size: 9px; }

@keyframes overview-service-flow {
  0% { opacity: 0; transform: translateX(0); }
  12%, 88% { opacity: 1; }
  100% { opacity: 0; transform: translateX(calc(82cqw - 7px)); }
}

@media (max-width: 860px) {
  .overview-connection-stage { grid-template-columns: minmax(0, 1fr); }
}

@media (max-width: 520px) {
  .overview-connection-stage { min-height: 0; padding: 20px 16px; }
  .overview-service-path { margin-top: 36px; }
  .overview-connection-metrics div { padding: 12px; }
}

@media (prefers-reduced-motion: reduce) {
  .overview-service-path.is-flowing .overview-service-track span {
    animation: none;
    opacity: .65;
  }
}
</style>
