<script setup lang="ts">
import { computed } from 'vue'
import CommandComposer from './CommandComposer.vue'
import CommandTimeline from './CommandTimeline.vue'
import type { DeviceMgmtListItem } from '../../types/api'
import type { BalanceQuery, CommandDefinition, CommandEvent } from '../../types/commands'
import { commandTargetDevice } from '../../utils/commandInput'
import { balanceResultText } from '../../utils/commandPresentation'
import {
  Bot24Regular,
  Delete24Regular,
  History24Regular,
  PlugConnected24Regular,
  Wallet24Regular
} from '@vicons/fluent'

const props = defineProps<{
  events: CommandEvent[]
  balanceQueries: BalanceQuery[]
  latestBalance?: BalanceQuery
  definitions: CommandDefinition[]
  devices: DeviceMgmtListItem[]
  selectedDevice: string
  loading: boolean
  loadingOlder: boolean
  hasOlder: boolean
  busy: boolean
  streamConnected: boolean
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

const emit = defineEmits<{
  'update:selectedDevice': [value: string]
  loadOlder: []
  clearHistory: []
  openBalance: []
  submit: [input: string]
  dangerous: [command: CommandDefinition]
}>()

function latestBalanceText(query?: BalanceQuery) {
  if (!query) return ''
  return balanceResultText(query)
}
</script>

<template>
  <section class="chat-shell ui-surface-strong" aria-label="VoHive 命令会话">
    <header class="chat-header">
      <div class="chat-identity">
        <span class="chat-avatar" aria-hidden="true"><el-icon><Bot24Regular /></el-icon></span>
        <div>
          <h3>VoHive 命令会话</h3>
          <div class="chat-meta">
            <span class="stream-state" :class="{ online: streamConnected }">
              <el-icon><PlugConnected24Regular /></el-icon>
              {{ streamConnected ? '实时连接' : '正在重连' }}
            </span>
            <span>{{ visibleEvents.length + visibleBalanceQueries.length }} 条消息</span>
            <span v-if="latestBalance" class="latest-balance">{{ latestBalanceText(latestBalance) }}</span>
          </div>
        </div>
      </div>

      <div class="chat-actions">
        <el-select
          :model-value="selectedDevice"
          class="chat-device-select"
          placeholder="选择设备"
          aria-label="命令目标设备"
          @update:model-value="emit('update:selectedDevice', String($event || ''))"
        >
          <el-option v-for="device in devices" :key="device.id" :label="device.name || device.id" :value="device.id" />
        </el-select>
        <el-tooltip content="余额查询与历史" placement="bottom">
          <el-button class="icon-action" aria-label="打开余额查询与历史" @click="emit('openBalance')">
            <el-icon><Wallet24Regular /></el-icon>
          </el-button>
        </el-tooltip>
        <el-tooltip v-if="hasOlder" content="读取更早记录" placement="bottom">
          <el-button class="icon-action" aria-label="读取更早记录" :loading="loadingOlder" @click="emit('loadOlder')">
            <el-icon><History24Regular /></el-icon>
          </el-button>
        </el-tooltip>
        <el-tooltip content="清除已结束的命令历史" placement="bottom">
          <el-button class="icon-action" aria-label="清除命令历史" @click="emit('clearHistory')">
            <el-icon><Delete24Regular /></el-icon>
          </el-button>
        </el-tooltip>
      </div>
    </header>

    <main class="chat-conversation">
      <CommandTimeline :events="visibleEvents" :balance-queries="visibleBalanceQueries" :loading="loading" />
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
.chat-shell { min-width: 0; min-height: 0; height: 100%; overflow: hidden; border-radius: 20px; display: grid; grid-template-rows: auto minmax(0, 1fr); box-shadow: var(--ui-shadow-sm); background: radial-gradient(circle at 78% 16%, color-mix(in srgb, var(--ui-primary) 7%, transparent), transparent 34%), var(--ui-surface); }
.chat-header { min-height: 78px; padding: 12px 16px; border-bottom: 1px solid var(--ui-border); display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.chat-identity, .chat-actions, .chat-meta, .stream-state { display: flex; align-items: center; }
.chat-identity { min-width: 0; gap: 10px; }
.chat-avatar { width: 42px; height: 42px; flex: 0 0 42px; border: 1px solid color-mix(in srgb, var(--ui-primary) 42%, var(--ui-border)); border-radius: 13px; background: color-mix(in srgb, var(--ui-primary) 10%, transparent); color: var(--ui-primary); display: grid; place-items: center; font-size: 21px; box-shadow: 0 0 24px color-mix(in srgb, var(--ui-primary) 10%, transparent); }
.chat-identity h3 { margin: 0; font-size: 15px; font-weight: 700; letter-spacing: 0; }
.chat-meta { margin-top: 3px; gap: 10px; color: #64748b; font-size: 11px; }
.latest-balance { max-width: 220px; color: var(--ui-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.stream-state { gap: 4px; color: #b45309; }
.stream-state.online { color: var(--ui-success); }
.chat-actions { flex: 0 0 auto; gap: 8px; }
.chat-device-select { width: 180px; }
.chat-actions :deep(.el-input__wrapper), .chat-actions :deep(.el-button) { min-height: 44px; }
.chat-actions :deep(.icon-action) { width: 44px; padding: 0; }
.chat-conversation { min-width: 0; min-height: 0; display: grid; grid-template-rows: minmax(0, 1fr) auto; }
@media (max-width: 640px) {
  .chat-header { align-items: stretch; flex-direction: column; gap: 10px; }
  .chat-actions { width: 100%; }
  .chat-device-select { width: auto; flex: 1 1 auto; min-width: 0; }
  .latest-balance { max-width: 150px; }
}
</style>
