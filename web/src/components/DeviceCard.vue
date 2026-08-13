<script setup lang="ts">
import { computed } from 'vue'
import {
  Cellular3G24Regular,
  Cellular4G24Regular,
  Cellular5G24Regular,
  CellularData124Regular,
  DataUsage24Regular,
  Globe24Regular,
  Wifi124Regular
} from '@vicons/fluent'
import type { DashboardDevice } from '../types/api'
import {
  createDashboardDevicePresentation,
  hasDashboardSignal
} from '../utils/dashboardPresentation'

const props = withDefaults(defineProps<{
  device: DashboardDevice
  selected?: boolean
}>(), {
  selected: false
})

const presentation = computed(() => createDashboardDevicePresentation(props.device))
const networkIcon = computed(() => {
  if (props.device.vowifi_active) return Wifi124Regular
  const mode = presentation.value.connectionType.toUpperCase()
  if (mode.includes('5G') || mode.includes('NR')) return Cellular5G24Regular
  if (mode.includes('4G') || mode.includes('LTE')) return Cellular4G24Regular
  if (mode.includes('3G') || mode.includes('WCDMA') || mode.includes('UMTS')) {
    return Cellular3G24Regular
  }
  return CellularData124Regular
})
const connectionDetail = computed(() => {
  if (!props.device.healthy) return '设备未连接'
  if (props.device.vowifi_active) return `${presentation.value.connectionType} · VoWiFi 已连接`
  if (presentation.value.connectionType === '不可用') return '网络检测中'
  return `${presentation.value.connectionType} 已连接`
})
const signalBars = computed(() => {
  const dbm = props.device.signal_dbm
  if (!hasDashboardSignal(dbm)) return 0
  if (dbm > -70) return 4
  if (dbm > -85) return 3
  if (dbm > -100) return 2
  return 1
})
const signalTone = computed(() => {
  if (signalBars.value === 0) return 'is-unavailable'
  if (signalBars.value === 1) return 'is-danger'
  if (signalBars.value === 2) return 'is-warning'
  return 'is-good'
})
const showCellularFacts = computed(() => presentation.value.showsCellularFacts)
</script>

<template>
  <button
    type="button"
    class="device-card ui-card ui-card-hover"
    :class="{ 'device-card-selected': selected, 'is-vowifi': !showCellularFacts }"
    :aria-label="`选择 ${presentation.displayName}；双击打开设备工作区`"
    :aria-pressed="selected"
  >
    <span class="device-card-header">
      <span class="device-title-group">
        <span class="device-glyph" aria-hidden="true"><component :is="networkIcon" /></span>
        <span class="device-identity">
          <strong>{{ presentation.displayName }}</strong>
          <small>{{ device.id }} · {{ presentation.operator }}</small>
        </span>
      </span>
      <span class="device-state" :class="device.healthy ? 'is-online' : 'is-offline'">
        <i aria-hidden="true" />
        {{ presentation.statusLabel }}
      </span>
    </span>

    <span class="device-connection-summary">
      <span class="connection-primary">
        <component :is="networkIcon" aria-hidden="true" />
        <span>
          <strong>{{ presentation.connectionTitle }}</strong>
          <small>{{ connectionDetail }}</small>
        </span>
      </span>
    </span>

    <span v-if="showCellularFacts" class="device-card-footer">
      <span class="device-addresses">
        <span class="address-heading">
          <Globe24Regular aria-hidden="true" />
          <span>公网地址</span>
        </span>
        <span class="address-list">
          <span>
            <small>IPv4</small>
            <code :title="presentation.ipv4">{{ presentation.ipv4 }}</code>
          </span>
          <span>
            <small>IPv6</small>
            <code :title="presentation.ipv6">{{ presentation.ipv6 }}</code>
          </span>
        </span>
      </span>
      <span class="device-signal">
        <span class="signal-label">
          <DataUsage24Regular aria-hidden="true" />
          <span>蜂窝信号</span>
        </span>
        <span class="signal-reading" :class="signalTone">
          <span class="signal-bars" aria-hidden="true">
            <i v-for="bar in 4" :key="bar" :class="{ 'is-filled': signalBars >= bar }" />
          </span>
          <strong>{{ presentation.signal }}</strong>
        </span>
      </span>
    </span>
  </button>
</template>

<style scoped>
.device-card { position: relative; min-width: 0; min-height: 188px; padding: 0; overflow: hidden; border-color: var(--ui-border); border-radius: 12px; background: linear-gradient(145deg, color-mix(in srgb, var(--ui-surface) 98%, var(--ui-primary) 2%), var(--ui-surface)); color: var(--ui-text); text-align: left; cursor: pointer; }
.device-card.is-vowifi { min-height: 112px; }
.device-card-selected { border-color: color-mix(in srgb, var(--ui-primary) 58%, var(--ui-border)); background: linear-gradient(150deg, color-mix(in srgb, var(--ui-surface) 84%, var(--ui-primary) 16%), var(--ui-surface) 62%); box-shadow: 0 0 0 1px color-mix(in srgb, var(--ui-primary) 8%, transparent), var(--ui-shadow-md); }
.device-card-header { min-height: 62px; padding: 10px 14px; display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.device-title-group { min-width: 0; display: flex; align-items: center; gap: 11px; }
.device-glyph { width: 36px; height: 36px; flex: 0 0 36px; display: grid; place-items: center; border: 1px solid color-mix(in srgb, var(--ui-primary) 35%, var(--ui-border)); border-radius: 9px; background: color-mix(in srgb, var(--ui-primary) 12%, transparent); color: var(--ui-primary); }
.device-glyph svg { width: 20px; height: 20px; }
.device-identity { min-width: 0; display: grid; }
.device-identity strong,
.device-identity small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.device-identity strong { font-size: 15px; font-weight: 700; }
.device-identity small { max-width: 190px; margin-top: 3px; color: var(--ui-text-muted); font-size: var(--ui-font-caption); }
.device-state { flex: 0 0 auto; display: inline-flex; align-items: center; gap: 7px; font-size: 12px; font-weight: 650; }
.device-state i { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
.device-state.is-online { color: var(--ui-success); }
.device-state.is-offline { color: var(--ui-danger); }
.device-state.is-online i { animation: device-online-pulse 2.4s var(--ui-ease-in-out) infinite; }

.device-connection-summary { min-height: 50px; padding: 8px 14px; display: flex; align-items: center; border-top: 1px solid var(--ui-border-muted); border-bottom: 1px solid var(--ui-border-muted); background: radial-gradient(ellipse at center, color-mix(in srgb, var(--ui-primary) 5%, transparent), transparent 70%); }
.connection-primary { min-width: 0; display: flex; align-items: center; gap: 10px; }
.connection-primary > svg { width: 21px; height: 21px; flex: 0 0 21px; color: var(--ui-primary); }
.connection-primary > span { min-width: 0; display: grid; gap: 3px; }
.connection-primary strong,
.connection-primary small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.connection-primary strong { font-size: 14px; }
.connection-primary small { color: var(--ui-text-muted); font-size: var(--ui-font-caption); }
.device-card-footer { min-height: 76px; padding: 8px 12px 8px 14px; display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: 10px; }
.device-addresses { min-width: 0; display: grid; }
.address-heading { display: flex; align-items: flex-start; gap: 7px; color: var(--ui-text-muted); font-size: var(--ui-font-caption); }
.address-heading svg { width: 16px; height: 16px; flex: 0 0 16px; }
.address-list { min-width: 0; margin-top: 4px; display: grid; gap: 2px; }
.address-list > span { min-width: 0; display: grid; grid-template-columns: 34px minmax(0, 1fr); gap: 6px; }
.address-list small { color: var(--ui-text-muted); font: var(--ui-font-caption)/1.45 "v-mono", monospace; }
.address-list code { min-width: 0; color: color-mix(in srgb, var(--ui-text) 82%, transparent); font: var(--ui-font-body-sm)/1.35 "v-mono", monospace; overflow-wrap: anywhere; }
.device-signal { align-self: center; display: grid; gap: 7px; }
.signal-label,
.signal-reading { display: flex; align-items: center; gap: 6px; }
.signal-label { color: var(--ui-text-muted); font-size: var(--ui-font-caption); }
.signal-label svg { width: 16px; height: 16px; }
.signal-reading { color: var(--ui-text-muted); }
.signal-reading.is-good { color: var(--ui-success); }
.signal-reading.is-warning { color: var(--ui-warning); }
.signal-reading.is-danger { color: var(--ui-danger); }
.signal-reading strong { font: var(--ui-font-body-sm) "v-mono", monospace; }
.signal-bars { width: 20px; height: 14px; display: flex; align-items: flex-end; gap: 2px; }
.signal-bars i { width: 3px; height: calc(var(--bar-index, 1) * 25%); min-height: 3px; border-radius: 1px; background: color-mix(in srgb, currentColor 24%, transparent); }
.signal-bars i:nth-child(1) { height: 25%; }
.signal-bars i:nth-child(2) { height: 50%; }
.signal-bars i:nth-child(3) { height: 75%; }
.signal-bars i:nth-child(4) { height: 100%; }
.signal-bars i.is-filled { background: currentColor; }

@keyframes device-online-pulse { 0%, 100% { opacity: .55; } 50% { opacity: 1; } }

@media (prefers-reduced-motion: reduce) {
  .device-state.is-online i { animation: none; }
}
</style>
