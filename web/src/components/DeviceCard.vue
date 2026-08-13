<script setup lang="ts">
import { computed } from 'vue'
import type { DashboardDevice } from '../types/api'
import StatusLight from './StatusLight.vue'
import {
  Cellular3G24Regular,
  Cellular4G24Regular,
  Cellular5G24Regular,
  CellularData124Regular,
  Wifi124Regular, 
  Globe24Regular
} from '@vicons/fluent'

const props = withDefaults(defineProps<{
  device: DashboardDevice
  selected?: boolean
}>(), {
  selected: false
})
const displayNetworkMode = computed(() => {
  const mode = String(props.device?.network_mode || '').trim()
  const duplex = String(props.device?.network_duplex || '').trim()
  if (!mode) return ''
  return duplex ? `${duplex} ${mode}` : mode
})

const networkIcon = computed(() => {
  // VoWiFi 模式显示 Wi-Fi 图标
  if (props.device?.vowifi_active) return Wifi124Regular
  const mode = displayNetworkMode.value
  if (!mode) return CellularData124Regular
  const m = String(mode).toUpperCase()
  if (m.includes('5G') || m.includes('NR')) return Cellular5G24Regular
  if (m.includes('4G') || m.includes('LTE')) return Cellular4G24Regular
  if (m.includes('3G') || m.includes('WCDMA') || m.includes('HSPA') || m.includes('UMTS')) return Cellular3G24Regular
  return CellularData124Regular
})

const networkColor = computed(() => {
  if (props.device?.vowifi_active) return 'text-emerald-500'
  const mode = displayNetworkMode.value
  if (!mode) return 'text-gray-400'
  const m = String(mode).toUpperCase()
  if (m.includes('5G') || m.includes('NR')) return 'text-blue-600'
  if (m.includes('4G') || m.includes('LTE')) return 'text-blue-500'
  if (m.includes('3G')) return 'text-orange-500'
  return 'text-gray-400'
})

function hasValidSignalDbm(dbm: number | null | undefined): dbm is number {
  return typeof dbm === 'number' && Number.isFinite(dbm) && dbm !== 0 && dbm !== -999
}

function getSignalColor(dbm: number | null | undefined) {
  if (!hasValidSignalDbm(dbm)) return 'bg-gray-300 dark:bg-gray-600'
  if (dbm > -70) return 'bg-green-500'
  if (dbm > -90) return 'bg-yellow-500'
  return 'bg-red-500'
}

function getSignalBars(dbm: number | null | undefined) {
  if (!hasValidSignalDbm(dbm)) return 0
  if (dbm > -70) return 4
  if (dbm > -85) return 3
  if (dbm > -100) return 2
  return 1
}

const deviceState = computed(() => {
  if (!props.device.healthy) return { label: '离线', tone: 'danger' as const }
  if (!props.device.vowifi_active && hasValidSignalDbm(props.device.signal_dbm) && props.device.signal_dbm <= -100) {
    return { label: '警告（信号弱）', tone: 'warning' as const }
  }
  return { label: '在线', tone: 'success' as const }
})

const connectionText = computed(() => {
  if (!props.device.healthy) return '设备未连接'
  if (props.device.vowifi_active) return 'VoWiFi 已连接'
  return displayNetworkMode.value ? `${displayNetworkMode.value} 已连接` : '网络检测中'
})

const displayIP = computed(() => props.device.public_ipv6 || props.device.public_ip || '')
</script>

<template>
  <button
    type="button"
    class="device-card ui-card ui-card-hover"
    :class="{ 'device-card-selected': selected }"
    :aria-pressed="selected"
  >
    <header class="device-card-header">
      <div class="device-title-group">
        <span class="device-glyph" aria-hidden="true"><component :is="networkIcon" /></span>
        <div>
          <h3>{{ device.name || device.id }}</h3>
          <p>{{ device.id }}</p>
        </div>
      </div>
      <div class="device-state" :class="`device-state-${deviceState.tone}`">
        <StatusLight :tone="deviceState.tone" size="md" :animated="device.healthy" />
        <span>{{ deviceState.label }}</span>
      </div>
    </header>

    <section class="device-connection-summary">
      <div class="connection-primary">
        <el-icon :class="networkColor" aria-hidden="true"><component :is="networkIcon" /></el-icon>
        <div>
          <strong>{{ device.vowifi_active ? 'Wi-Fi Calling' : (device.operator || '网络检测中') }}</strong>
          <span>{{ connectionText }}<template v-if="!device.vowifi_active && displayNetworkMode"> · {{ displayNetworkMode }}</template></span>
        </div>
      </div>
      <div v-if="!device.vowifi_active" class="connection-signal" title="蜂窝信号强度">
        <span class="signal-bars" aria-hidden="true">
          <i
            v-for="i in 4"
            :key="i"
            :class="getSignalBars(device.signal_dbm) >= i ? getSignalColor(device.signal_dbm) : 'bg-gray-200 dark:bg-gray-700'"
            :style="{ height: `${i * 25}%` }"
          />
        </span>
        <span>{{ hasValidSignalDbm(device.signal_dbm) ? `${device.signal_dbm} dBm` : '--' }}</span>
      </div>
    </section>

    <dl class="device-facts">
      <div>
        <dt>公网 IP</dt>
        <dd class="device-public-ip" :title="displayIP">
          <el-icon aria-hidden="true"><Globe24Regular /></el-icon>
          {{ displayIP || '--' }}
        </dd>
      </div>
    </dl>
  </button>
</template>

<style scoped>
.device-card {
  min-width: 0;
  min-height: 214px;
  padding: 0;
  overflow: hidden;
  border-left: 1px solid var(--ui-border);
  background: var(--ui-surface);
  color: var(--ui-text);
  text-align: left;
  cursor: pointer;
}

.device-card:focus-visible {
  outline: 2px solid var(--ui-primary);
  outline-offset: 2px;
}

.device-card-selected {
  border-color: color-mix(in srgb, var(--ui-primary) 60%, var(--ui-border));
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--ui-primary) 20%, transparent), var(--ui-shadow-md);
}

.device-card-header {
  min-height: 78px;
  padding: 14px 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border-bottom: 1px solid var(--ui-border);
}

.device-title-group {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 11px;
}

.device-glyph {
  width: 40px;
  height: 40px;
  flex: 0 0 40px;
  display: grid;
  place-items: center;
  border: 1px solid color-mix(in srgb, var(--ui-primary) 28%, var(--ui-border));
  border-radius: 11px;
  background: color-mix(in srgb, var(--ui-primary) 8%, transparent);
  color: var(--ui-primary);
}

.device-glyph svg { width: 20px; }

.device-card-header h3 {
  min-width: 0;
  margin: 0;
  overflow: hidden;
  font-size: 15px;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.device-card-header p {
  max-width: 170px;
  margin: 3px 0 0;
  overflow: hidden;
  color: var(--ui-text-muted);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.device-state {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  font-weight: 600;
}

.device-state-success { color: var(--ui-success); }
.device-state-warning { color: var(--ui-warning); }
.device-state-danger { color: var(--ui-danger); }

.device-facts {
  margin: 0;
}

.device-connection-summary {
  min-height: 88px;
  padding: 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  border-bottom: 1px solid var(--ui-border-muted);
  background:
    radial-gradient(ellipse at center, color-mix(in srgb, var(--ui-primary) 10%, transparent), transparent 68%);
}

.connection-primary {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 11px;
}

.connection-primary > .el-icon {
  flex: 0 0 auto;
  font-size: 22px;
}

.connection-primary div {
  min-width: 0;
  display: grid;
  gap: 4px;
}

.connection-primary strong {
  overflow: hidden;
  color: var(--ui-text);
  font-size: 14px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.connection-primary span,
.connection-signal {
  color: var(--ui-text-muted);
  font-size: 11px;
}

.connection-signal {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: 7px;
}

.device-facts > div {
  min-height: 39px;
  padding: 8px 15px;
  display: grid;
  grid-template-columns: 66px minmax(0, 1fr);
  align-items: center;
  gap: 8px;
  border-bottom: 1px solid var(--ui-border-muted);
}

.device-facts > div:last-child {
  border-bottom: 0;
}

.device-facts dt {
  color: var(--ui-text-muted);
  font-size: 12px;
  font-weight: 600;
}

.device-facts dd {
  min-width: 0;
  margin: 0;
  overflow: hidden;
  color: var(--ui-text);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.device-public-ip {
  display: flex;
  align-items: center;
  gap: 7px;
}

.signal-bars {
  width: 22px;
  height: 16px;
  display: flex;
  align-items: flex-end;
  gap: 2px;
}

.signal-bars i {
  width: 4px;
  border-radius: 1px;
}

.device-public-ip {
  color: var(--ui-communication);
  font-family: "v-mono", ui-monospace, monospace;
  font-variant-numeric: tabular-nums;
}

.connection-value .el-icon,
.device-public-ip .el-icon {
  flex: 0 0 auto;
}
</style>
