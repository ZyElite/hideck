<script setup lang="ts">
import { computed } from 'vue'
import EmptyState from './EmptyState.vue'
import ListSkeleton from './ListSkeleton.vue'
import StatusLight from './StatusLight.vue'
import type { DeviceMgmtListItem } from '../types/api'
import { isControlOnline, isRadioRegistered, lifecycleStatusLabel, primaryLifecycleStatus } from '../utils/deviceLifecycle'

const props = defineProps<{
  loading: boolean
  query: string
  statusFilter: 'all' | 'online' | 'offline'
  sortKey: 'name' | 'signal'
  sortDir: 'asc' | 'desc'
  selectedId: string
  filteredDevices: DeviceMgmtListItem[]
  deviceCount: number
  deviceLimit: number
}>()

const emit = defineEmits<{
  'update:query': [value: string]
  'update:statusFilter': [value: 'all' | 'online' | 'offline']
  'update:sortKey': [value: 'name' | 'signal']
  'update:sortDir': [value: 'asc' | 'desc']
  'select-device': [id: string]
}>()

const modelQuery = computed({
  get: () => props.query,
  set: (value: string) => emit('update:query', value)
})

const modelStatusFilter = computed({
  get: () => props.statusFilter,
  set: (value: 'all' | 'online' | 'offline') => emit('update:statusFilter', value)
})



const modelSortKey = computed({
  get: () => props.sortKey,
  set: (value: 'name' | 'signal') => emit('update:sortKey', value)
})

const modelSortDir = computed({
  get: () => props.sortDir,
  set: (value: 'asc' | 'desc') => emit('update:sortDir', value)
})

const primaryStatus = primaryLifecycleStatus

const registrationText = (d: DeviceMgmtListItem) => {
  const phaseText = lifecycleStatusLabel(d.lifecycle_phase)
  if (phaseText && d.lifecycle_phase !== 'online' && d.lifecycle_phase !== 'offline') return phaseText
  if (isRadioRegistered(d)) {
    return `${d?.modem?.operator || '--'} · ${[d?.modem?.network_duplex, d?.modem?.network_mode].filter(Boolean).join(' ') || '--'}`
  }
  if (!isControlOnline(d)) return '控制面恢复中'
  if (d.registration_state_label === 'searching') return '搜索网络中'
  if (d.registration_state_label === 'denied') return '驻网被拒'
  return '未驻网'
}

const dataNetworkText = (d: DeviceMgmtListItem) => {
  if (d?.vowifi_enabled) return ''
  if (!d?.network_enabled) return '数据未开启'
  if (!d?.network_connected) return '数据网络未连接'
  return ''
}

const secondaryStatus = (d: DeviceMgmtListItem) => {
  if (d?.vowifi_enabled) return 'WiFi-Calling'
  return [registrationText(d), dataNetworkText(d)].filter(Boolean).join(' · ')
}
</script>

<template>
  <aside class="device-list-panel ui-card">
    <header class="device-rail-header">
      <div>
        <span>DEVICE RAIL</span>
        <h2>设备轨道</h2>
      </div>
      <strong>{{ filteredDevices.length }}<small> / {{ deviceCount }}</small></strong>
    </header>

    <div class="device-rail-search">
      <el-input v-model="modelQuery" placeholder="搜索设备、ICCID 或接口" clearable />
    </div>

    <div class="device-rail-filters">
      <el-select v-model="modelStatusFilter" size="small" placeholder="在线">
        <el-option label="全部状态" value="all" />
        <el-option label="仅在线" value="online" />
        <el-option label="仅离线" value="offline" />
      </el-select>

      <el-select v-model="modelSortKey" size="small" placeholder="排序">
        <el-option label="排序：名称" value="name" />
        <el-option label="排序：信号" value="signal" />
      </el-select>
      <el-select v-model="modelSortDir" size="small" placeholder="方向" class="device-sort-direction">
        <el-option label="升序" value="asc" />
        <el-option label="降序" value="desc" />
      </el-select>
    </div>

    <div v-if="deviceLimit > 0" class="device-quota" :class="{ 'is-full': deviceCount >= deviceLimit }">
      <span>设备配额</span>
      <strong>{{ deviceCount }} / {{ deviceLimit }}</strong>
    </div>

    <div class="device-rail-list">
      <ListSkeleton v-if="loading && filteredDevices.length === 0" :rows="8" />

      <EmptyState v-else-if="filteredDevices.length === 0" title="暂无设备" subtitle="点击右上角“添加设备”开始接管" />

      <div v-else class="device-list-scroll">
        <div class="device-list-grid">
          <div v-for="d in filteredDevices" :key="d.id" class="device-list-item">
          <button
            type="button"
            class="device-list-button w-full h-full text-left"
            :class="selectedId === d.id
              ? 'device-list-button-active'
              : 'device-list-button-idle'"
            @click="emit('select-device', d.id)"
          >
            <div class="device-list-button-main">
              <div class="min-w-0">
                <div class="font-bold text-gray-800 dark:text-gray-100 truncate">{{ d.name || d.id }}</div>
                <div class="text-xs text-gray-500 mt-0.5 truncate">
                  {{ d.id }} · {{ d?.interface || '--' }}
                </div>
                <div class="text-xs text-gray-400 mt-1 truncate">
                  {{ secondaryStatus(d) }}
                </div>
              </div>
              <div class="device-list-status">
                <StatusLight :tone="primaryStatus(d).tone" size="sm" :animated="primaryStatus(d).animated" />
                <span>{{ primaryStatus(d).label }}</span>
              </div>
            </div>
          </button>
          </div>
        </div>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.device-list-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 7px;
}

.device-list-item {
  min-width: 0;
}

.device-list-panel {
  position: sticky;
  top: 16px;
  align-self: start;
  max-height: calc(100dvh - 112px);
  padding: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--ui-surface);
}

.device-rail-header {
  min-height: 82px;
  padding: 17px 18px;
  display: flex;
  align-items: end;
  justify-content: space-between;
  border-bottom: 1px solid var(--ui-border);
}

.device-rail-header span {
  color: var(--ui-primary);
  font: 700 9px "v-mono", monospace;
  letter-spacing: .14em;
}

.device-rail-header h2 {
  margin: 5px 0 0;
  color: var(--ui-text);
  font-size: 18px;
  font-weight: 650;
}

.device-rail-header > strong {
  color: var(--ui-text);
  font: 24px "v-mono", monospace;
}

.device-rail-header small {
  color: var(--ui-text-muted);
  font-size: 11px;
}

.device-rail-search {
  padding: 14px 14px 9px;
}

.device-rail-filters {
  padding: 0 14px 12px;
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) 78px;
  gap: 7px;
}

.device-quota {
  margin: 0 14px 10px;
  padding: 8px 10px;
  display: flex;
  justify-content: space-between;
  border: 1px solid var(--ui-border-muted);
  border-radius: 9px;
  color: var(--ui-text-muted);
  font-size: 10px;
}

.device-quota.is-full strong {
  color: var(--ui-warning);
}

.device-rail-list {
  min-height: 180px;
  padding: 4px 8px 9px;
  overflow: hidden;
}

.device-list-scroll {
  max-height: calc(100dvh - 350px);
  overflow-y: auto;
  padding: 0 2px;
}

.device-list-button {
  min-height: 91px;
  padding: 13px 12px;
  border: 1px solid transparent;
  border-radius: 11px;
  transition: background-color 150ms ease, border-color 150ms ease, transform 120ms ease-out;
}

.device-list-button:active {
  transform: scale(.985);
}

.device-list-button-main {
  display: flex;
  align-items: start;
  justify-content: space-between;
  gap: 10px;
}

.device-list-status {
  display: flex;
  align-items: center;
  gap: 5px;
  color: var(--ui-text-muted);
  font-size: 10px;
  white-space: nowrap;
}

.device-list-button-active {
  border-color: color-mix(in srgb, var(--ui-primary) 42%, var(--ui-border));
  background: color-mix(in srgb, var(--ui-primary) 8%, var(--ui-surface));
  box-shadow: inset 2px 0 0 var(--ui-primary);
}

.device-list-button-active::after {
  position: absolute;
  top: 50%;
  right: 10px;
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--ui-primary);
  box-shadow: 0 0 12px var(--ui-primary);
  content: "";
  transform: translateY(-50%);
}

.device-list-button-idle {
  background: var(--ui-surface);
}

.device-list-button {
  position: relative;
}

.device-list-button-idle:hover {
  border-color: var(--ui-border);
  background: var(--ui-surface-muted);
}

@media (max-width: 979px) {
  .device-list-panel {
    position: static;
    max-height: none;
  }

  .device-list-scroll {
    max-height: 330px;
  }
}
</style>
