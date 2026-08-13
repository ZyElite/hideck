<script setup lang="ts">
import { computed } from 'vue'
import type { DeviceMgmtListItem } from '../../types/api'
import type { BalanceQuery, CarrierQueryRule } from '../../types/commands'
import { isControlOnline } from '../../utils/deviceLifecycle'
import { formatDeviceDateTime } from '../../utils/deviceTime'
import {
  balanceResultText,
  balanceTransportLabel,
  presentBalanceState
} from '../../utils/commandPresentation'
import {
  CheckmarkCircle24Regular,
  Clock24Regular,
  Edit24Regular,
  ErrorCircle24Regular,
  Info24Regular,
  Phone24Regular,
  Wallet24Regular
} from '@vicons/fluent'

const props = defineProps<{
  devices: DeviceMgmtListItem[]
  selectedDevice: string
  queries: BalanceQuery[]
  builtInRules: CarrierQueryRule[]
  customRules: CarrierQueryRule[]
  loading: boolean
  querying: boolean
}>()

const emit = defineEmits<{
  'update:selectedDevice': [value: string]
  query: []
  editRules: []
}>()

const selectedQueries = computed(() => [...props.queries
  .filter((query) => !props.selectedDevice || query.device_id === props.selectedDevice)]
  .sort((left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at)))
const latestQuery = computed(() => selectedQueries.value[0])
const visibleRules = computed(() => [...props.customRules, ...props.builtInRules]
  .filter((rule) => rule.enabled)
  .slice(0, 4))

function deviceLabel(device: DeviceMgmtListItem): string {
  return `${device.name || device.id} · ${isControlOnline(device) ? '在线' : '离线'}`
}

function ruleRoute(rule: CarrierQueryRule): string {
  if (rule.transport === 'unsupported') return rule.alternative || '不支持自动查询'
  const payload = rule.payload || '未提供内容'
  const destination = rule.destination || (rule.response_mode === 'direct' ? '直接返回' : '未提供目标')
  return `${payload} → ${destination}`
}
</script>

<template>
  <section class="balance-panel" aria-label="运营商余额">
    <header class="panel-heading">
      <span class="panel-icon" aria-hidden="true"><el-icon><Wallet24Regular /></el-icon></span>
      <div>
        <h2>余额查询</h2>
        <span>真实运营商回复与解析记录</span>
      </div>
      <el-tooltip content="编辑运营商规则" placement="left">
        <el-button text aria-label="编辑运营商规则" @click="emit('editRules')">
          <el-icon><Edit24Regular /></el-icon>
        </el-button>
      </el-tooltip>
    </header>

    <div class="query-controls">
      <label for="balance-device">选择设备</label>
      <div class="query-row">
        <el-select
          id="balance-device"
          :model-value="selectedDevice"
          placeholder="选择设备"
          class="device-select"
          aria-label="余额查询设备"
          @update:model-value="emit('update:selectedDevice', String($event || ''))"
        >
          <template #prefix><el-icon><Phone24Regular /></el-icon></template>
          <el-option v-for="device in devices" :key="device.id" :label="deviceLabel(device)" :value="device.id" />
        </el-select>
        <el-button type="primary" :loading="querying" :disabled="!selectedDevice" @click="emit('query')">
          查询
        </el-button>
      </div>
    </div>

    <section class="latest-result" aria-label="最新余额结果">
      <span class="section-label">最新结果</span>
      <template v-if="latestQuery">
        <strong>{{ balanceResultText(latestQuery) }}</strong>
        <span class="latest-meta">
          <el-icon><Clock24Regular /></el-icon>
          {{ formatDeviceDateTime(latestQuery.updated_at) }} · 来源 {{ balanceTransportLabel(latestQuery) }}
        </span>
      </template>
      <span v-else class="latest-empty">当前设备暂无余额记录</span>
    </section>

    <section class="history-section" aria-label="余额历史">
      <h3>历史记录 <span>{{ selectedQueries.length }}</span></h3>
      <div class="balance-history" aria-live="polite">
        <div v-if="loading" class="balance-empty">正在读取余额记录</div>
        <div v-else-if="!selectedQueries.length" class="balance-empty">暂无查询记录</div>
        <article v-for="query in selectedQueries" :key="query.id" class="balance-item">
          <span class="history-icon" :class="presentBalanceState(query).tone" aria-hidden="true">
            <el-icon v-if="presentBalanceState(query).tone === 'danger'"><ErrorCircle24Regular /></el-icon>
            <el-icon v-else-if="['running', 'waiting'].includes(presentBalanceState(query).tone)"><Clock24Regular /></el-icon>
            <el-icon v-else><CheckmarkCircle24Regular /></el-icon>
          </span>
          <div>
            <b :class="`tone-${presentBalanceState(query).tone}`">{{ presentBalanceState(query).label }}</b>
            <small>{{ query.device_id }} · {{ balanceTransportLabel(query) }}</small>
          </div>
          <span class="history-result">
            {{ balanceResultText(query) }}
            <small>{{ formatDeviceDateTime(query.updated_at) }}</small>
          </span>
          <pre v-if="query.raw_response">{{ query.raw_response }}</pre>
          <p v-if="query.error" class="query-error">{{ query.error }}</p>
        </article>
      </div>
    </section>

    <section class="rules-section" aria-label="运营商规则">
      <h3>运营商规则 <span>{{ builtInRules.length + customRules.length }}</span></h3>
      <div v-if="visibleRules.length" class="rule-list">
        <article v-for="rule in visibleRules" :key="rule.id">
          <div>
            <strong>{{ rule.operator || rule.id }}</strong>
            <small>{{ rule.built_in ? '内置' : '自定义' }}</small>
          </div>
          <span>{{ ruleRoute(rule) }}</span>
          <el-tooltip :content="`${rule.mcc}/${rule.mnc} · ${rule.id}`" placement="left">
            <el-icon aria-label="规则详情"><Info24Regular /></el-icon>
          </el-tooltip>
        </article>
      </div>
      <button v-else type="button" class="rules-empty" @click="emit('editRules')">
        暂无可用规则，打开规则编辑器
      </button>
    </section>
  </section>
</template>

<style scoped>
.balance-panel { min-width: 0; min-height: 0; height: 100%; padding: 18px; overflow: auto; }
.panel-heading { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 10px; }
.panel-icon { width: 36px; height: 36px; border-radius: 6px; background: color-mix(in srgb, var(--ui-primary) 12%, transparent); color: var(--ui-primary); display: grid; place-items: center; }
.panel-icon .el-icon { font-size: 19px; }
.panel-heading h2 { margin: 0; color: var(--ui-text); font-size: 18px; }
.panel-heading div > span { color: var(--ui-text-subtle); font-size: 10px; }
.panel-heading :deep(.el-button) { width: 36px; height: 36px; padding: 0; }
.query-controls { display: grid; gap: 7px; margin: 18px 0 16px; }
.query-controls > label, .section-label { color: var(--ui-text-muted); font-size: 11px; }
.query-row { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 7px; }
.device-select { min-width: 0; }
.query-row :deep(.el-input__wrapper), .query-row :deep(.el-button) { min-height: 40px; border-radius: 4px; }
.latest-result { display: flex; flex-direction: column; gap: 6px; padding: 15px 0; border-block: 1px solid var(--ui-border); }
.latest-result > strong { color: var(--ui-primary); font-size: clamp(25px, 2.4vw, 36px); font-weight: 550; overflow-wrap: anywhere; }
.latest-meta { color: var(--ui-text-subtle); display: flex; align-items: center; gap: 5px; font-size: 10px; }
.latest-empty { min-height: 42px; color: var(--ui-text-subtle); display: flex; align-items: center; font-size: 11px; }
.balance-panel h3 { margin: 17px 0 7px; color: var(--ui-text); font-size: 13px; }
.balance-panel h3 span { margin-left: 4px; color: var(--ui-text-subtle); font-weight: 400; }
.balance-history, .rule-list { border-top: 1px solid var(--ui-border); }
.balance-item { display: grid; grid-template-columns: auto minmax(0, 1fr) minmax(90px, auto); gap: 8px; align-items: center; padding: 9px 0; border-bottom: 1px solid var(--ui-border); }
.history-icon { width: 28px; height: 28px; border-radius: 50%; display: grid; place-items: center; color: var(--ui-primary); background: color-mix(in srgb, currentColor 10%, transparent); }
.history-icon.waiting, .history-icon.running, .tone-waiting, .tone-running { color: var(--ui-warning); }
.history-icon.parsed, .tone-parsed { color: var(--ui-info); }
.history-icon.danger, .tone-danger, .query-error { color: var(--ui-danger); }
.balance-item b { font-size: 11px; }
.balance-item small { display: block; margin-top: 2px; color: var(--ui-text-subtle); font-size: 9px; }
.history-result { min-width: 0; color: var(--ui-text); font-size: 11px; text-align: right; overflow-wrap: anywhere; }
.balance-item pre, .balance-item .query-error { grid-column: 2 / -1; margin: 0; overflow-wrap: anywhere; }
.balance-item pre { max-height: 90px; overflow: auto; color: var(--ui-text-muted); font: 10px/1.5 "v-mono", monospace; white-space: pre-wrap; }
.query-error { font-size: 10px; }
.balance-empty { min-height: 90px; color: var(--ui-text-subtle); display: grid; place-items: center; font-size: 11px; }
.rule-list article { min-height: 48px; padding: 9px 8px; border: 1px solid var(--ui-border); border-top: 0; display: grid; grid-template-columns: minmax(90px, 1fr) minmax(0, auto) auto; align-items: center; gap: 8px; }
.rule-list article > div { min-width: 0; display: flex; align-items: center; gap: 5px; }
.rule-list strong { min-width: 0; color: var(--ui-text); font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rule-list small { color: var(--ui-primary); font-size: 8px; }
.rule-list article > span { color: var(--ui-text-muted); font: 9px "v-mono", monospace; text-align: right; overflow-wrap: anywhere; }
.rule-list article > .el-icon { color: var(--ui-text-subtle); }
.rules-empty { width: 100%; min-height: 48px; border: 1px solid var(--ui-border); border-radius: 4px; background: transparent; color: var(--ui-text-muted); font-size: 11px; }
@media (max-width: 1023px) {
  .balance-panel { min-height: 560px; }
  .latest-result > strong { font-size: 34px; }
}
@media (max-width: 640px) {
  .balance-panel { padding: 16px 12px 88px; }
  .query-row :deep(.el-input__wrapper), .query-row :deep(.el-button) { min-height: 44px; }
}
</style>
