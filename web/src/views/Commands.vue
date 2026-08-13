<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import CommandChat from '../components/commands/CommandChat.vue'
import BalanceDrawer from '../components/commands/BalanceDrawer.vue'
import RuleEditorDrawer from '../components/commands/RuleEditorDrawer.vue'
import { useEventStream } from '../composables/useEventStream'
import { useCommandRuntimeStatus } from '../composables/useCommandRuntimeStatus'
import { commandService } from '../services/commands'
import { devicesService } from '../services/devices'
import type { CarrierQueryRule, CommandDefinition, CommandEvent } from '../types/commands'
import { isCarrierRuleOperationBlocked } from '../utils/carrierRuleRuntime'
import { buildDangerousCommand } from '../utils/commandInput'

const pageSize = 100
const definitions = ref<CommandDefinition[]>([])
const events = ref<CommandEvent[]>([])
const builtInRules = ref<CarrierQueryRule[]>([])
const customRules = ref<CarrierQueryRule[]>([])
const selectedDevice = ref('')
const loading = ref(true)
const loadingOlder = ref(false)
const hasOlder = ref(false)
const historyVersion = ref(0)
const refreshVersion = ref(0)
const manualRefreshing = ref(false)
const executing = ref(false)
const querying = ref(false)
const rulesOpen = ref(false)
const balanceOpen = ref(false)
const savingRule = ref(false)
const deletingRuleID = ref('')
const rulesLoading = ref(false)
const rulesLoaded = ref(false)
const rulesError = ref('')
const ruleOperationBlocked = computed(() => isCarrierRuleOperationBlocked({
  loading: rulesLoading.value,
  saving: savingRule.value,
  deletingId: deletingRuleID.value
}))
const dangerousDefinition = ref<CommandDefinition | null>(null)
const dangerForm = reactive({ device: '', target: '', phone: '', duration: 15 })
let disposed = false
let rulesRequestID = 0

const runtimeStatus = useCommandRuntimeStatus({
  fetchDevices: () => devicesService.listManaged(),
  fetchBalances: () => commandService.balances({ limit: 50 }),
  onForegroundError: (message) => ElMessage.error(message)
})
const devices = runtimeStatus.devices
const balances = runtimeStatus.balances

const stream = useEventStream<CommandEvent>({
  path: '/command-center/stream',
  eventName: 'command',
  parse: (payload) => JSON.parse(payload) as CommandEvent,
  onEvent: (event) => mergeEvents([event]),
  reconnectDelayMs: 2500
})
const streamConnected = stream.connected
const syncWarning = computed(() => [
  stream.lastError.value ? `实时事件：${stream.lastError.value}` : '',
  runtimeStatus.syncWarning.value
].filter(Boolean).join('；'))

watch(devices, (nextDevices) => {
  if (nextDevices.some((device) => device.id === selectedDevice.value)) return
  selectedDevice.value = nextDevices[0]?.id || ''
}, { flush: 'sync' })

const dangerousTitle = computed(() => {
  if (dangerousDefinition.value?.name === 'switch') return '切换 eSIM'
  if (dangerousDefinition.value?.name === 'vocall') return 'VoWiFi 通话'
  return '切换公网 IP'
})

onMounted(async () => {
  const pageData = Promise.all([loadCatalog(), runtimeStatus.refresh(), loadRules()])
  await loadEvents()
  if (disposed) return
  const latest = events.value.at(-1)?.id
  if (latest) stream.setLastEventId(latest)
  void stream.connect()
  await pageData
  if (disposed) return
  runtimeStatus.startPolling()
  loading.value = false
})

onUnmounted(() => {
  disposed = true
  stream.disconnect()
  runtimeStatus.dispose()
})

async function loadCatalog() {
  const result = await commandService.catalog()
  if (result.ok) definitions.value = result.data
  else ElMessage.error(result.error.message || '命令目录加载失败')
}

async function loadEvents() {
  const latestBeforeRequest = events.value.at(-1)?.id || 0
  const result = await commandService.events({ beforeId: 0, limit: pageSize })
  if (!result.ok) {
    ElMessage.error(result.error.message || '命令历史加载失败')
    return false
  }
  const liveEvents = events.value.filter((event) => event.id > latestBeforeRequest)
  events.value = mergeEventLists(result.data, liveEvents)
  hasOlder.value = result.data.length === pageSize
  return true
}

async function loadOlder() {
  const firstID = events.value[0]?.id
  if (!firstID || loadingOlder.value) return
  loadingOlder.value = true
  const result = await commandService.events({ beforeId: firstID, limit: pageSize })
  loadingOlder.value = false
  if (!result.ok) {
    ElMessage.error(result.error.message || '更早记录加载失败')
    return
  }
  mergeEvents(result.data)
  historyVersion.value += 1
  hasOlder.value = result.data.length === pageSize
}

async function loadRules() {
  if (ruleOperationBlocked.value) return false
  const requestID = ++rulesRequestID
  rulesLoading.value = true
  rulesError.value = ''
  const result = await commandService.rules()
  if (requestID !== rulesRequestID) return false
  rulesLoading.value = false
  if (!result.ok) {
    rulesError.value = result.error.message || '运营商规则加载失败'
    ElMessage.error(rulesError.value)
    return false
  }
  rulesLoaded.value = true
  builtInRules.value = result.data.builtIn
  customRules.value = result.data.custom
  return true
}

async function execute(input: string) {
  if (executing.value) return
  executing.value = true
  const result = await commandService.execute(input)
  executing.value = false
  if (!result.ok) {
    ElMessage.error(result.error.message || '命令执行失败')
    return
  }
  const latest = events.value.at(-1)?.id || 0
  const catchup = await commandService.events({ afterId: latest, limit: 20 })
  if (catchup.ok) mergeEvents(catchup.data)
  if (input.trim().split(/\s+/, 1)[0]?.toLowerCase() === '/balance') {
    await runtimeStatus.refresh({ background: true })
  }
}

function mergeEvents(incoming: CommandEvent[]) {
  events.value = mergeEventLists(events.value, incoming)
}

function mergeEventLists(...lists: CommandEvent[][]) {
  const merged = new Map<number, CommandEvent>()
  for (const list of lists) {
    for (const event of list) merged.set(event.id, event)
  }
  return [...merged.values()].sort((left, right) => left.id - right.id)
}

async function refreshAll() {
  if (manualRefreshing.value) return
  manualRefreshing.value = true
  try {
    const [eventsLoaded] = await Promise.all([
      loadEvents(),
      runtimeStatus.refresh(),
      loadCatalog(),
      loadRules()
    ])
    if (eventsLoaded) refreshVersion.value += 1
  } finally {
    manualRefreshing.value = false
  }
}

async function clearHistory() {
  const confirmed = await ElMessageBox.confirm('只清除已完成和失败的记录，进行中的命令会保留。', '清除命令历史', {
    confirmButtonText: '清除', cancelButtonText: '取消', type: 'warning'
  }).then(() => true).catch(() => false)
  if (!confirmed) return
  const result = await commandService.clearHistory()
  if (!result.ok) {
    ElMessage.error(result.error.message || '清除失败')
    return
  }
  events.value = events.value.filter((event) => event.execution?.state === 'running')
  ElMessage.success(`已清除 ${result.data} 条执行记录`)
}

async function startBalance() {
  if (!selectedDevice.value || querying.value) return
  querying.value = true
  const result = await commandService.startBalance(selectedDevice.value)
  querying.value = false
  if (!result.ok) {
    ElMessage.error(result.error.message || '余额查询失败')
    return
  }
  balances.value = [result.data, ...balances.value.filter((item) => item.id !== result.data.id)]
  balanceOpen.value = false
  ElMessage.success(result.data.state === 'completed' ? '运营商已返回结果' : '查询已发送，正在等待运营商回复')
}

function openDangerous(definition: CommandDefinition) {
  dangerousDefinition.value = definition
  dangerForm.device = selectedDevice.value || devices.value[0]?.id || ''
  dangerForm.target = ''
  dangerForm.phone = ''
  dangerForm.duration = 15
}

async function confirmDangerous() {
  const definition = dangerousDefinition.value
  if (!definition) return
  let command = ''
  try {
    command = buildDangerousCommand({ name: definition.name, ...dangerForm })
  } catch (error) {
    ElMessage.warning(error instanceof Error ? error.message : '快捷动作参数无效')
    return
  }
  const confirmed = await ElMessageBox.confirm(command, `确认${dangerousTitle.value}`, {
    confirmButtonText: '执行', cancelButtonText: '取消', type: 'warning'
  }).then(() => true).catch(() => false)
  if (!confirmed) return
  dangerousDefinition.value = null
  await execute(command)
}

async function saveRule(rule: CarrierQueryRule, updating: boolean) {
  if (ruleOperationBlocked.value) return
  savingRule.value = true
  const result = updating
    ? await commandService.updateRule(rule.id, rule)
    : await commandService.createRule(rule)
  if (!result.ok) {
    savingRule.value = false
    ElMessage.error(result.error.message || '规则保存失败')
    return
  }
  savingRule.value = false
  const refreshed = await loadRules()
  if (refreshed) ElMessage.success(updating ? '自定义规则已更新' : '自定义规则已创建')
}

async function deleteRule(id: string) {
  if (ruleOperationBlocked.value) return
  const confirmed = await ElMessageBox.confirm(`删除自定义规则 ${id}？`, '删除规则', {
    confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning'
  }).then(() => true).catch(() => false)
  if (!confirmed) return
  if (ruleOperationBlocked.value) return
  deletingRuleID.value = id
  const result = await commandService.deleteRule(id)
  if (!result.ok) {
    deletingRuleID.value = ''
    ElMessage.error(result.error.message || '规则删除失败')
    return
  }
  deletingRuleID.value = ''
  const refreshed = await loadRules()
  if (refreshed) ElMessage.success('自定义规则已删除')
}
</script>

<template>
  <div class="app-page commands-page">
    <div class="commands-layout">
      <CommandChat
        v-model:selected-device="selectedDevice"
        :events="events"
        :balance-queries="balances"
        :definitions="definitions"
        :devices="devices"
        :loading="loading"
        :loading-older="loadingOlder"
        :has-older="hasOlder"
        :history-version="historyVersion"
        :refresh-version="refreshVersion"
        :busy="executing"
        :stream-connected="streamConnected"
        :refreshing="manualRefreshing || runtimeStatus.refreshing.value"
        :sync-warning="syncWarning"
        :last-synced-at="runtimeStatus.lastSyncedAt.value"
        @refresh="refreshAll"
        @load-older="loadOlder"
        @clear-history="clearHistory"
        @open-balance="balanceOpen = true"
        @submit="execute"
        @dangerous="openDangerous"
      />
      <BalanceDrawer
        v-model="balanceOpen"
        v-model:selected-device="selectedDevice"
        :devices="devices"
        :queries="balances"
        :built-in-rules="builtInRules"
        :custom-rules="customRules"
        :loading="loading"
        :querying="querying"
        :rules-loading="rulesLoading"
        :rules-loaded="rulesLoaded"
        :rules-error="rulesError"
        @query="startBalance"
        @edit-rules="rulesOpen = true"
        @refresh-rules="loadRules"
      />
    </div>

    <el-dialog
      :model-value="!!dangerousDefinition"
      :title="dangerousTitle"
      width="min(460px, 92vw)"
      append-to-body
      @update:model-value="(open) => { if (!open) dangerousDefinition = null }"
    >
      <el-form label-position="top" @submit.prevent="confirmDangerous">
        <el-form-item label="设备" required>
          <el-select v-model="dangerForm.device" class="w-full">
            <el-option v-for="device in devices" :key="device.id" :label="device.name || device.id" :value="device.id" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="dangerousDefinition?.name === 'switch'" label="Profile 序号或 ICCID" required>
          <el-input v-model="dangerForm.target" />
        </el-form-item>
        <template v-if="dangerousDefinition?.name === 'vocall'">
          <el-form-item label="电话号码" required><el-input v-model="dangerForm.phone" /></el-form-item>
          <el-form-item label="通话秒数"><el-input-number v-model="dangerForm.duration" :min="1" :max="300" /></el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="dangerousDefinition = null">取消</el-button>
        <el-button type="warning" @click="confirmDangerous">检查并执行</el-button>
      </template>
    </el-dialog>

    <RuleEditorDrawer
      v-model="rulesOpen"
      :built-in="builtInRules"
      :custom="customRules"
      :saving="savingRule"
      :loading="rulesLoading"
      :loaded="rulesLoaded"
      :error="rulesError"
      :deleting-id="deletingRuleID"
      @save="saveRule"
      @delete="deleteRule"
      @refresh="loadRules"
    />
  </div>
</template>

<style scoped>
.commands-page { min-width: 0; min-height: 0; }
.commands-layout {
  min-width: 0;
  height: calc(100dvh - 112px);
  min-height: 540px;
  overflow: hidden;
  border: 1px solid var(--ui-border);
  border-radius: 8px;
  background: var(--ui-surface);
  display: grid;
  grid-template-columns: minmax(0, 1fr) clamp(300px, 25vw, 360px);
}
.commands-layout :deep(.chat-shell) { border: 0; border-radius: 0; }
@media (max-width: 1023px) {
  .commands-layout { height: auto; min-height: 0; grid-template-columns: minmax(0, 1fr); overflow: visible; }
  .commands-layout :deep(.chat-shell) { height: 690px; min-height: 690px; }
}
@media (max-width: 640px) {
  .commands-layout { margin: 0 -4px; }
  .commands-layout :deep(.chat-shell) { height: 720px; min-height: 720px; }
}
</style>
