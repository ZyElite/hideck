<script setup lang="ts">
import { computed } from 'vue'
import CommandComposer from './CommandComposer.vue'
import CommandTimeline from './CommandTimeline.vue'
import type { DeviceMgmtListItem } from '../../types/api'
import type { BalanceQuery, CommandDefinition, CommandEvent } from '../../types/commands'
import { commandTargetDevice } from '../../utils/commandInput'
import {
  Chat24Regular,
  Delete24Regular,
  History24Regular,
  Phone24Regular,
  PlugConnected24Regular,
  ArrowSync24Regular,
  ErrorCircle24Regular,
  Wallet24Regular
} from '@vicons/fluent'
import { formatDeviceTime } from '../../utils/deviceTime'

const props = defineProps<{
  events: CommandEvent[]
  balanceQueries: BalanceQuery[]
  definitions: CommandDefinition[]
  devices: DeviceMgmtListItem[]
  selectedDevice: string
  loading: boolean
  loadingOlder: boolean
  hasOlder: boolean
  historyVersion: number
  refreshVersion: number
  busy: boolean
  streamConnected: boolean
  streamStarted: boolean
  refreshing: boolean
  syncWarning: string
  lastSyncedAt: number | null
}>()

const visibleEvents = computed(() => props.events.filter((event) => {
  if (!props.selectedDevice) return true
  const target = commandTargetDevice(event.execution?.input || '', props.definitions)
  return target === null || target === props.selectedDevice
}))

const visibleBalanceQueries = computed(() => props.balanceQueries.filter((query) => (
  !props.selectedDevice || query.device_id === props.selectedDevice
)))
const deviceIds = computed(() => props.devices.map((device) => device.id))
const visibleRecordCount = computed(() => visibleEvents.value.length + visibleBalanceQueries.value.length)
const syncLabel = computed(() => props.lastSyncedAt
  ? `更新 ${formatDeviceTime(props.lastSyncedAt, { clientClock: true, fallback: '--:--:--' })}`
  : '等待状态同步'
)

const emit = defineEmits<{
  'update:selectedDevice': [value: string]
  loadOlder: []
  clearHistory: []
  openBalance: []
  refresh: []
  submit: [input: string]
  dangerous: [command: CommandDefinition]
}>()
</script>

<template>
  <section class="chat-shell" aria-label="HiDeck 命令会话">
    <header class="chat-header">
      <div class="chat-heading">
        <span class="chat-title-icon" aria-hidden="true"><el-icon><Chat24Regular /></el-icon></span>
        <div>
          <div class="chat-title-row">
            <h2>HiDeck 命令会话</h2>
            <span class="stream-state" :class="{ online: streamConnected }" aria-live="polite">
              <el-icon><PlugConnected24Regular /></el-icon>
              {{ streamConnected ? '实时连接' : streamStarted ? '正在重连' : '实时连接已暂停' }}
            </span>
          </div>
          <span class="record-count">当前设备 · {{ visibleRecordCount }} 条记录 · {{ syncLabel }}</span>
        </div>
      </div>

      <div class="chat-actions">
        <el-tooltip content="刷新命令与状态" placement="bottom">
          <el-button
            :loading="refreshing"
            :disabled="refreshing"
            aria-label="刷新命令与状态"
            @click="emit('refresh')"
          >
            <el-icon><ArrowSync24Regular /></el-icon><span>刷新</span>
          </el-button>
        </el-tooltip>
        <el-tooltip content="余额查询与历史" placement="bottom">
          <el-button aria-label="余额查询与历史" @click="emit('openBalance')">
            <el-icon><Wallet24Regular /></el-icon><span>余额历史</span>
          </el-button>
        </el-tooltip>
        <el-tooltip v-if="hasOlder" content="读取更早记录" placement="bottom">
          <el-button :loading="loadingOlder" aria-label="读取更早记录" @click="emit('loadOlder')">
            <el-icon><History24Regular /></el-icon><span>更早记录</span>
          </el-button>
        </el-tooltip>
        <el-tooltip content="清除已结束的命令历史" placement="bottom">
          <el-button aria-label="清除命令历史" @click="emit('clearHistory')">
            <el-icon><Delete24Regular /></el-icon><span>清除历史</span>
          </el-button>
        </el-tooltip>
      </div>

      <label class="device-target">
        <span class="device-label">目标设备</span>
        <el-select
          :model-value="selectedDevice"
          placeholder="选择设备"
          aria-label="命令目标设备"
          @update:model-value="emit('update:selectedDevice', String($event || ''))"
        >
          <template #prefix><el-icon><Phone24Regular /></el-icon></template>
          <el-option v-for="device in devices" :key="device.id" :label="device.name || device.id" :value="device.id" />
        </el-select>
      </label>

      <p v-if="syncWarning" class="sync-warning" role="status">
        <el-icon aria-hidden="true"><ErrorCircle24Regular /></el-icon>
        <span>{{ syncWarning }}</span>
      </p>
    </header>

    <main class="chat-conversation">
      <CommandTimeline
        :events="visibleEvents"
        :balance-queries="visibleBalanceQueries"
        :loading="loading"
        :context-key="`${selectedDevice}:${refreshVersion}`"
        :history-version="historyVersion"
      />
      <CommandComposer
        :definitions="definitions"
        :busy="busy"
        :selected-device="selectedDevice"
        :device-ids="deviceIds"
        @submit="emit('submit', $event)"
        @dangerous="emit('dangerous', $event)"
      />
    </main>
  </section>
</template>

<style scoped>
.chat-shell {
  min-width: 0;
  min-height: 0;
  height: 100%;
  overflow: hidden;
  border: 1px solid var(--ui-border);
  border-radius: 8px;
  background: var(--ui-surface);
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
}
.chat-header {
  padding: 16px 18px 14px;
  border-bottom: 1px solid var(--ui-border);
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  grid-template-areas: "heading actions" "device actions" "warning warning";
  align-items: start;
  gap: 12px 18px;
}
.chat-heading { min-width: 0; grid-area: heading; display: flex; align-items: center; gap: 11px; }
.chat-title-icon {
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  border: 1px solid color-mix(in srgb, var(--ui-info) 55%, var(--ui-border));
  border-radius: 7px;
  color: var(--ui-info);
  display: grid;
  place-items: center;
}
.chat-title-icon .el-icon { font-size: 19px; }
.chat-title-row, .stream-state { display: flex; align-items: center; }
.chat-title-row { min-width: 0; flex-wrap: wrap; gap: 8px 12px; }
.chat-title-row h2 { margin: 0; color: var(--ui-text); font-size: 19px; font-weight: 700; }
.stream-state { gap: 5px; color: var(--ui-warning); font-size: var(--ui-font-caption); }
.stream-state.online { color: var(--ui-success); }
.record-count { display: block; margin-top: 3px; color: var(--ui-text-subtle); font: var(--ui-font-caption) "v-mono", monospace; }
.chat-actions { grid-area: actions; display: flex; align-items: center; justify-content: flex-end; gap: 0; }
.chat-actions :deep(.el-button) { min-height: 36px; margin: 0; border-radius: 0; }
.chat-actions :deep(.el-button + .el-button) { margin-left: -1px; }
.chat-actions :deep(.el-button:first-child) { border-radius: 4px 0 0 4px; }
.chat-actions :deep(.el-button:last-child) { border-radius: 0 4px 4px 0; }
.chat-actions :deep(.el-button span) { display: inline-flex; align-items: center; gap: 6px; }
.device-target { width: 220px; min-width: 0; grid-area: device; display: grid; gap: 5px; }
.device-label { color: var(--ui-text-subtle); font-size: var(--ui-font-caption); }
.device-target :deep(.el-input__wrapper) { min-height: 36px; border-radius: 4px; }
.sync-warning {
  min-width: 0;
  margin: 0;
  grid-area: warning;
  color: var(--ui-danger);
  font-size: var(--ui-font-body-sm);
  display: flex;
  align-items: flex-start;
  gap: 6px;
}
.sync-warning .el-icon { flex: 0 0 auto; margin-top: 1px; }
.sync-warning span { overflow-wrap: anywhere; }
.chat-conversation { min-width: 0; min-height: 0; display: grid; grid-template-rows: minmax(0, 1fr) auto; }
@media (max-width: 900px) {
  .chat-header { grid-template-columns: minmax(0, 1fr) auto; }
  .chat-actions :deep(.el-button) { width: 40px; padding: 0; }
  .chat-actions :deep(.el-button span span) { display: none; }
}
@media (max-width: 640px) {
  .chat-header {
    padding: 14px 12px 12px;
    grid-template-columns: minmax(0, 1fr);
    grid-template-areas: "heading" "device" "actions" "warning";
    gap: 11px;
  }
  .chat-title-row h2 { font-size: 17px; }
  .device-target { width: 100%; }
  .chat-actions { justify-content: flex-start; }
  .chat-actions :deep(.el-button) { min-height: 44px; width: 44px; }
}
</style>
