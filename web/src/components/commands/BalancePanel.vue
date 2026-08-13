<script setup lang="ts">
import type { BalanceQuery } from '../../types/commands'
import type { DeviceMgmtListItem } from '../../types/api'
import { formatDeviceDateTime } from '../../utils/deviceTime'
import { balanceResultText, presentBalanceState } from '../../utils/commandPresentation'
import { Edit24Regular, Wallet24Regular } from '@vicons/fluent'

defineProps<{
  devices: DeviceMgmtListItem[]
  selectedDevice: string
  queries: BalanceQuery[]
  loading: boolean
  querying: boolean
}>()

const emit = defineEmits<{
  'update:selectedDevice': [value: string]
  query: []
  editRules: []
}>()

</script>

<template>
  <aside class="balance-panel" aria-label="运营商余额">
    <div class="panel-heading">
      <div>
        <h3>运营商余额</h3>
      </div>
      <el-tooltip content="编辑运营商规则" placement="left">
        <el-button text aria-label="编辑运营商规则" @click="emit('editRules')">
          <el-icon><Edit24Regular /></el-icon>
        </el-button>
      </el-tooltip>
    </div>
    <div class="query-controls">
      <el-select
        :model-value="selectedDevice"
        placeholder="选择设备"
        class="device-select"
        @update:model-value="emit('update:selectedDevice', String($event || ''))"
      >
        <el-option v-for="device in devices" :key="device.id" :label="device.name || device.id" :value="device.id" />
      </el-select>
      <el-button type="primary" :loading="querying" :disabled="!selectedDevice" @click="emit('query')">
        查询
      </el-button>
    </div>

    <div class="balance-list" aria-live="polite">
      <div v-if="loading" class="balance-empty">正在读取余额记录</div>
      <div v-else-if="!queries.length" class="balance-empty">
        <el-icon><Wallet24Regular /></el-icon>
        <span>暂无查询记录</span>
      </div>
      <article v-for="query in queries" :key="query.id" class="balance-item">
        <div class="balance-row">
          <strong>{{ balanceResultText(query) }}</strong>
          <span class="query-state" :class="presentBalanceState(query).tone">
            {{ presentBalanceState(query).label }}
          </span>
        </div>
        <div class="balance-meta">
          <span>{{ query.device_id }}</span>
          <time>{{ formatDeviceDateTime(query.updated_at) }}</time>
        </div>
        <pre v-if="query.raw_response">{{ query.raw_response }}</pre>
        <p v-if="query.error" class="query-error">{{ query.error }}</p>
      </article>
    </div>
  </aside>
</template>

<style scoped>
.balance-panel { min-width: 0; min-height: 0; height: 100%; display: flex; flex-direction: column; }
.panel-heading { min-height: 58px; padding: 10px 12px 10px 14px; border-bottom: 1px solid var(--ui-border); display: flex; align-items: center; justify-content: space-between; }
.panel-heading h3 { margin: 0; font-size: 15px; letter-spacing: 0; }
.query-controls { padding: 12px; display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 8px; border-bottom: 1px solid var(--ui-border); }
.device-select { min-width: 0; }
.query-controls :deep(.el-input__wrapper), .query-controls :deep(.el-button) { min-height: 44px; }
.balance-list { min-height: 0; overflow: auto; }
.balance-item { padding: 13px 14px; border-bottom: 1px solid var(--ui-border); }
.balance-row, .balance-meta { display: flex; justify-content: space-between; gap: 8px; }
.balance-row strong { min-width: 0; font-size: 14px; overflow-wrap: anywhere; }
.balance-meta { margin-top: 5px; color: #94a3b8; font-size: 11px; }
.query-state { flex: 0 0 auto; color: #64748b; font-size: 11px; }
.query-state.completed { color: #15803d; }
.query-state.failed, .query-state.timed_out, .query-error { color: #dc2626; }
.balance-item pre { margin: 10px 0 0; padding: 8px; max-height: 130px; overflow: auto; border-radius: 6px; background: rgba(15, 23, 42, .05); font: 11px/1.5 "v-mono", monospace; white-space: pre-wrap; overflow-wrap: anywhere; }
.query-error { margin: 7px 0 0; font-size: 11px; }
.balance-empty { min-height: 180px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px; color: #94a3b8; font-size: 12px; }
.balance-empty .el-icon { font-size: 26px; color: #0d9488; }
</style>
