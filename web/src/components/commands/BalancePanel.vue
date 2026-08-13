<script setup lang="ts">
import { computed } from 'vue'
import type { DeviceMgmtListItem } from '../../types/api'
import type { BalanceQuery, CarrierQueryRule } from '../../types/commands'
import { isControlOnline } from '../../utils/deviceLifecycle'
import { formatDeviceDateTime } from '../../utils/deviceTime'
import { effectiveCarrierRules } from '../../utils/carrierRuleRuntime'
import {
  balanceResultText,
  balanceTransportLabel,
  presentBalanceState
} from '../../utils/commandPresentation'
import {
  ArrowSync24Regular,
  Chat24Regular,
  CheckmarkCircle24Regular,
  Clock24Regular,
  Database24Regular,
  Edit24Regular,
  ErrorCircle24Regular,
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
  rulesLoading: boolean
  rulesLoaded: boolean
  rulesError: string
}>()

const emit = defineEmits<{
  'update:selectedDevice': [value: string]
  query: []
  editManualBalance: []
  editRules: []
  editRule: [rule: CarrierQueryRule]
  refreshRules: []
}>()

const selectedQueries = computed(() => [...props.queries
  .filter((query) => !props.selectedDevice || query.device_id === props.selectedDevice)]
  .sort((left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at)))
const manualQuery = computed(() => selectedQueries.value.find((query) => query.transport === 'manual'))
const latestQuery = computed(() => manualQuery.value || selectedQueries.value[0])
const visibleRules = computed(() => effectiveCarrierRules(props.builtInRules, props.customRules).slice(0, 4))

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
      <el-button text class="manage-rules-button" @click="emit('editRules')">
        <el-icon><Edit24Regular /></el-icon><span>管理规则</span>
      </el-button>
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
      </div>
      <div class="query-actions">
        <el-button type="primary" :loading="querying" :disabled="!selectedDevice" @click="emit('query')">自动查询</el-button>
        <el-button :disabled="!selectedDevice || querying" @click="emit('editManualBalance')">
          <el-icon><Edit24Regular /></el-icon>{{ manualQuery ? '编辑手动余额' : '手动设置' }}
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
            <el-icon v-else-if="presentBalanceState(query).tone === 'parsed'"><Chat24Regular /></el-icon>
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
      <div class="rules-heading">
        <h3>运营商规则 <span>{{ rulesLoaded ? builtInRules.length + customRules.length : '—' }}</span></h3>
        <el-button text :loading="rulesLoading" aria-label="刷新运营商规则" @click="emit('refreshRules')">
          <el-icon v-if="!rulesLoading"><ArrowSync24Regular /></el-icon><span>刷新</span>
        </el-button>
      </div>
      <div class="rule-source">
        <el-icon aria-hidden="true"><Database24Regular /></el-icon>
        <div>
          <strong>后端规则库</strong>
          <span>内置可覆盖编辑 · 数据库自定义可管理</span>
        </div>
        <div class="source-counts">
          <small>内置 {{ rulesLoaded ? builtInRules.length : '—' }}</small>
          <small>自定义 {{ rulesLoaded ? customRules.length : '—' }}</small>
        </div>
      </div>
      <div v-if="rulesLoading && !rulesLoaded" class="rules-state">正在从后端读取规则</div>
      <div v-else-if="rulesError" class="rules-state rules-error" role="alert">
        <span>{{ rulesError }}</span>
        <el-button text @click="emit('refreshRules')">重试</el-button>
      </div>
      <div v-else-if="visibleRules.length" class="rule-list">
        <button v-for="rule in visibleRules" :key="rule.id" type="button" @click="emit('editRule', rule)">
          <div>
            <strong>{{ rule.operator || rule.id }}</strong>
            <small>{{ rule.built_in ? '服务端内置' : '数据库自定义' }}</small>
          </div>
          <span>{{ ruleRoute(rule) }}</span>
          <el-tooltip :content="`${rule.mcc}/${rule.mnc} · ${rule.id} · 点击编辑`" placement="left">
            <el-icon aria-label="编辑规则"><Edit24Regular /></el-icon>
          </el-tooltip>
        </button>
      </div>
      <button v-else-if="rulesLoaded" type="button" class="rules-empty" @click="emit('editRules')">
        后端当前没有可用规则，打开管理器
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
.panel-heading div > span { color: var(--ui-text-subtle); font-size: var(--ui-font-caption); }
.manage-rules-button { min-height: 36px; padding-inline: 8px; color: var(--ui-primary); }
.manage-rules-button span { font-size: var(--ui-font-caption); }
.query-controls { display: grid; gap: 7px; margin: 18px 0 16px; }
.query-controls > label, .section-label { color: var(--ui-text-muted); font-size: var(--ui-font-caption); }
.query-row { display: grid; grid-template-columns: minmax(0, 1fr); }
.query-actions { display: grid; grid-template-columns: 1fr 1fr; gap: 7px; }
.device-select { min-width: 0; }
.query-row :deep(.el-input__wrapper), .query-actions :deep(.el-button) { min-height: 40px; border-radius: 4px; }
.latest-result { display: flex; flex-direction: column; gap: 6px; padding: 15px 0; border-block: 1px solid var(--ui-border); }
.latest-result > strong { color: var(--ui-primary); font-size: clamp(25px, 2.4vw, 36px); font-weight: 550; overflow-wrap: anywhere; }
.latest-meta { color: var(--ui-text-subtle); display: flex; align-items: center; gap: 5px; font-size: var(--ui-font-caption); }
.latest-empty { min-height: 42px; color: var(--ui-text-subtle); display: flex; align-items: center; font-size: var(--ui-font-body-sm); }
.balance-panel h3 { margin: 17px 0 7px; color: var(--ui-text); font-size: 13px; }
.balance-panel h3 span { margin-left: 4px; color: var(--ui-text-subtle); font-weight: 400; }
.balance-history, .rule-list { border-top: 1px solid var(--ui-border); }
.balance-item { display: grid; grid-template-columns: auto minmax(0, 1fr) minmax(90px, auto); gap: 8px; align-items: center; padding: 9px 0; border-bottom: 1px solid var(--ui-border); }
.history-icon { width: 28px; height: 28px; border-radius: 50%; display: grid; place-items: center; color: var(--ui-success); background: color-mix(in srgb, currentColor 10%, transparent); }
.history-icon.waiting, .history-icon.running, .tone-waiting, .tone-running { color: var(--ui-warning); }
.history-icon.parsed, .tone-parsed { color: var(--ui-info); }
.history-icon.manual, .tone-manual { color: var(--ui-primary); }
.history-icon.success, .tone-success { color: var(--ui-success); }
.history-icon.danger, .tone-danger, .query-error { color: var(--ui-danger); }
.balance-item b { font-size: var(--ui-font-body-sm); }
.balance-item small { display: block; margin-top: 2px; color: var(--ui-text-subtle); font-size: var(--ui-font-caption); }
.history-result { min-width: 0; color: var(--ui-text); font-size: var(--ui-font-body-sm); text-align: right; overflow-wrap: anywhere; }
.balance-item pre, .balance-item .query-error { grid-column: 2 / -1; margin: 0; overflow-wrap: anywhere; }
.balance-item pre { max-height: 90px; overflow: auto; color: var(--ui-text-muted); font: var(--ui-font-body-sm)/1.5 "v-mono", monospace; white-space: pre-wrap; }
.query-error { font-size: var(--ui-font-body-sm); }
.balance-empty { min-height: 90px; color: var(--ui-text-subtle); display: grid; place-items: center; font-size: var(--ui-font-caption); }
.rules-heading { display: flex; align-items: end; justify-content: space-between; gap: 8px; }
.rules-heading :deep(.el-button) { min-height: 36px; margin-bottom: 1px; padding-inline: 7px; color: var(--ui-text-muted); }
.rules-heading :deep(.el-button span) { font-size: var(--ui-font-caption); }
.rule-source { min-width: 0; padding: 10px 8px; border-block: 1px solid var(--ui-border); display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 8px; }
.rule-source > .el-icon { color: var(--ui-primary); font-size: 18px; }
.rule-source strong, .rule-source span { display: block; }
.rule-source strong { color: var(--ui-text); font-size: var(--ui-font-body-sm); }
.rule-source span { margin-top: 2px; color: var(--ui-text-subtle); font-size: var(--ui-font-caption); }
.source-counts { display: flex; gap: 5px; }
.source-counts small { padding: 3px 5px; border: 1px solid var(--ui-border); border-radius: 3px; color: var(--ui-text-muted); font-size: var(--ui-font-caption); white-space: nowrap; }
.rules-state { min-height: 72px; padding: 12px; border-bottom: 1px solid var(--ui-border); color: var(--ui-text-subtle); display: flex; align-items: center; justify-content: center; gap: 8px; font-size: var(--ui-font-caption); text-align: center; }
.rules-error { color: var(--ui-danger); flex-direction: column; }
.rules-error :deep(.el-button) { color: var(--ui-danger); }
.rule-list button { width: 100%; min-height: 48px; padding: 9px 8px; border: 1px solid var(--ui-border); border-top: 0; background: transparent; color: inherit; display: grid; grid-template-columns: minmax(90px, 1fr) minmax(0, auto) auto; align-items: center; gap: 8px; text-align: left; cursor: pointer; }
.rule-list button:hover, .rule-list button:focus-visible { background: color-mix(in srgb, var(--ui-primary) 7%, transparent); outline: none; box-shadow: inset 2px 0 var(--ui-primary); }
.rule-list button > div { min-width: 0; display: flex; align-items: center; gap: 5px; }
.rule-list strong { min-width: 0; color: var(--ui-text); font-size: var(--ui-font-body-sm); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rule-list small { color: var(--ui-primary); font-size: var(--ui-font-caption); white-space: nowrap; }
.rule-list button > span { color: var(--ui-text-muted); font: var(--ui-font-body-sm) "v-mono", monospace; text-align: right; overflow-wrap: anywhere; }
.rule-list button > .el-icon { color: var(--ui-primary); }
.rules-empty { width: 100%; min-height: 48px; border: 1px solid var(--ui-border); border-radius: 4px; background: transparent; color: var(--ui-text-muted); font-size: var(--ui-font-body-sm); }
@media (max-width: 1023px) {
  .balance-panel { min-height: 560px; }
  .latest-result > strong { font-size: 34px; }
}
@media (max-width: 640px) {
  .balance-panel { padding: 16px 12px 88px; }
  .query-row :deep(.el-input__wrapper), .query-actions :deep(.el-button) { min-height: 44px; }
  .manage-rules-button, .rules-heading :deep(.el-button), .rules-state :deep(.el-button) { min-height: 44px; }
  .rule-source { grid-template-columns: auto minmax(0, 1fr); }
  .source-counts { grid-column: 2; flex-wrap: wrap; }
}
</style>
