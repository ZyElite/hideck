<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import CommandChat from '../components/commands/CommandChat.vue'
import BalanceDrawer from '../components/commands/BalanceDrawer.vue'
import RuleEditorDrawer from '../components/commands/RuleEditorDrawer.vue'
import ManualBalanceDialog from '../components/commands/ManualBalanceDialog.vue'
import { useEventStream } from '../composables/useEventStream'
import { useCommandRuntimeStatus } from '../composables/useCommandRuntimeStatus'
import { commandService } from '../services/commands'
import { devicesService } from '../services/devices'
import type { BalanceQuery, CarrierQueryRule, CommandDefinition, CommandEvent, ManualBalanceInput } from '../types/commands'
import { isCarrierRuleOperationBlocked } from '../utils/carrierRuleRuntime'
import { buildDangerousCommand } from '../utils/commandInput'
import { createDeviceRequestScope } from '../utils/deviceRequestScope'

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
const rulesInitialTab = ref<'custom' | 'builtin'>('custom')
const selectedRule = ref<CarrierQueryRule | null>(null)
const balanceOpen = ref(false)
const manualBalanceOpen = ref(false)
const manualBalanceOpening = ref(false)
const manualBalanceDeviceID = ref('')
const manualBalanceDialogExisting = ref<BalanceQuery | null>(null)
const manualBalanceSaving = ref(false)
const manualBalanceClearing = ref(false)
const savingRule = ref(false)
const deletingRuleID = ref('')
const rulesLoading = ref(false)
const rulesLoaded = ref(false)
const rulesError = ref('')
const manualBalance = ref<BalanceQuery | null>(null)
const manualBalanceError = ref('')
const ruleOperationBlocked = computed(() => isCarrierRuleOperationBlocked({
  loading: rulesLoading.value,
  saving: savingRule.value,
  deletingId: deletingRuleID.value
}))
const dangerousDefinition = ref<CommandDefinition | null>(null)
const dangerForm = reactive({ device: '', target: '', phone: '', duration: 15 })
let disposed = false
let rulesRequestID = 0
const manualBalanceRequestScope = createDeviceRequestScope('')

const runtimeStatus = useCommandRuntimeStatus({
  fetchDevices: () => devicesService.listManaged(),
  fetchBalances: () => commandService.balances({ limit: 50 }),
  onForegroundError: (message) => ElMessage.error(message)
})
const devices = runtimeStatus.devices
const balances = runtimeStatus.balances
const manualBalanceDevice = computed(() => devices.value.find((device) => device.id === manualBalanceDeviceID.value))
const selectedManualBalance = computed(() => manualBalance.value?.device_id === selectedDevice.value
  ? manualBalance.value
  : undefined)
const displayedBalances = computed(() => {
  const withoutSelectedManual = balances.value.filter((query) => (
    query.device_id !== selectedDevice.value || query.transport !== 'manual'
  ))
  return selectedManualBalance.value
    ? [selectedManualBalance.value, ...withoutSelectedManual]
    : withoutSelectedManual
})

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
  runtimeStatus.syncWarning.value,
  manualBalanceError.value ? `手动余额：${manualBalanceError.value}` : ''
].filter(Boolean).join('；'))

watch(devices, (nextDevices) => {
  if (nextDevices.some((device) => device.id === selectedDevice.value)) return
  selectedDevice.value = nextDevices[0]?.id || ''
}, { flush: 'sync' })

watch(selectedDevice, () => {
  manualBalanceRequestScope.invalidate(selectedDevice.value)
  manualBalance.value = null
  void loadManualBalance()
}, { flush: 'sync' })

watch(runtimeStatus.lastSyncedAt, () => {
  if (manualBalanceOpening.value) return
  void loadManualBalance()
})

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
    if (!manualBalanceOpening.value) await loadManualBalance(true)
  } finally {
    manualRefreshing.value = false
  }
}

async function loadManualBalance(showError = false, deviceID = selectedDevice.value) {
  const request = manualBalanceRequestScope.begin(deviceID)
  if (!deviceID) {
    manualBalance.value = null
    manualBalanceError.value = ''
    return true
  }
  const result = await commandService.manualBalance(deviceID)
  if (!manualBalanceRequestScope.isCurrent(request, selectedDevice.value)) return false
  if (!result.ok) {
    manualBalanceError.value = result.error.message || '手动余额读取失败'
    if (showError) ElMessage.error(manualBalanceError.value)
    return false
  }
  manualBalance.value = result.data
  manualBalanceError.value = ''
  return true
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

async function saveManualBalance(input: ManualBalanceInput) {
  const operationDeviceID = manualBalanceDeviceID.value
  if (!operationDeviceID || manualBalanceSaving.value) return
  manualBalanceSaving.value = true
  const result = await commandService.setManualBalance(operationDeviceID, input)
  manualBalanceSaving.value = false
  if (!result.ok) {
    ElMessage.error(result.error.message || '手动余额保存失败')
    return
  }
  balances.value = [result.data, ...balances.value.filter((item) => item.id !== result.data.id)]
  if (selectedDevice.value === operationDeviceID) {
    manualBalanceRequestScope.invalidate(operationDeviceID)
    manualBalance.value = result.data
    manualBalanceError.value = ''
  }
  if (manualBalanceDeviceID.value === operationDeviceID) {
    manualBalanceDialogExisting.value = result.data
    manualBalanceOpen.value = false
  }
  ElMessage.success('手动余额已保存')
}

async function clearManualBalance() {
  const operationDeviceID = manualBalanceDeviceID.value
  if (!operationDeviceID || manualBalanceClearing.value) return
  const confirmed = await ElMessageBox.confirm(`清除 ${operationDeviceID} 的手动余额后，将恢复显示自动查询记录。`, '清除手动余额', {
    confirmButtonText: '清除', cancelButtonText: '取消', type: 'warning'
  }).then(() => true).catch(() => false)
  if (!confirmed || manualBalanceDeviceID.value !== operationDeviceID) return
  manualBalanceClearing.value = true
  const result = await commandService.clearManualBalance(operationDeviceID)
  manualBalanceClearing.value = false
  if (!result.ok) {
    ElMessage.error(result.error.message || '手动余额清除失败')
    return
  }
  balances.value = balances.value.filter((item) => (
    item.device_id !== operationDeviceID || item.transport !== 'manual'
  ))
  if (selectedDevice.value === operationDeviceID) {
    manualBalanceRequestScope.invalidate(operationDeviceID)
    manualBalance.value = null
    manualBalanceError.value = ''
  }
  if (manualBalanceDeviceID.value === operationDeviceID) {
    manualBalanceDialogExisting.value = null
    manualBalanceOpen.value = false
  }
  ElMessage.success('手动余额已清除')
}

async function openManualBalance() {
  const operationDeviceID = selectedDevice.value
  if (!operationDeviceID || manualBalanceOpening.value) return
  manualBalanceOpening.value = true
  const loaded = await loadManualBalance(true, operationDeviceID)
  manualBalanceOpening.value = false
  if (!loaded || selectedDevice.value !== operationDeviceID) return
  manualBalanceDeviceID.value = operationDeviceID
  manualBalanceDialogExisting.value = manualBalance.value
  manualBalanceOpen.value = true
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
  await removeRule(id, '自定义规则已删除')
}

async function restoreBuiltInRule(id: string) {
  await removeRule(id, '已恢复服务端内置规则')
}

async function removeRule(id: string, successMessage: string) {
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
  if (refreshed) ElMessage.success(successMessage)
}

function openRuleEditor(rule: CarrierQueryRule | null = null, initialTab: 'custom' | 'builtin' = 'custom') {
  selectedRule.value = rule
  rulesInitialTab.value = initialTab
  rulesOpen.value = true
}

function openBuiltInRuleEditor() {
  openRuleEditor(null, 'builtin')
}

function updateRulesOpen(open: boolean) {
  rulesOpen.value = open
  if (!open) selectedRule.value = null
}
</script>

<template>
  <div class="app-page commands-page">
    <div class="commands-layout">
      <CommandChat
        v-model:selected-device="selectedDevice"
        :events="events"
        :balance-queries="displayedBalances"
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
        :queries="displayedBalances"
        :built-in-rules="builtInRules"
        :custom-rules="customRules"
        :loading="loading"
        :querying="querying"
        :manual-balance-opening="manualBalanceOpening"
        :rules-loading="rulesLoading"
        :rules-loaded="rulesLoaded"
        :rules-error="rulesError"
        @query="startBalance"
        @edit-manual-balance="openManualBalance"
        @edit-rules="openRuleEditor()"
        @edit-built-in-rules="openBuiltInRuleEditor"
        @edit-rule="openRuleEditor"
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
      :model-value="rulesOpen"
      :built-in="builtInRules"
      :custom="customRules"
      :initial-rule="selectedRule"
      :initial-tab="rulesInitialTab"
      :saving="savingRule"
      :loading="rulesLoading"
      :loaded="rulesLoaded"
      :error="rulesError"
      :deleting-id="deletingRuleID"
      @update:model-value="updateRulesOpen"
      @save="saveRule"
      @delete="deleteRule"
      @restore="restoreBuiltInRule"
      @refresh="loadRules"
    />

    <ManualBalanceDialog
      v-model="manualBalanceOpen"
      :device="manualBalanceDevice"
      :existing="manualBalanceDialogExisting || undefined"
      :saving="manualBalanceSaving"
      :clearing="manualBalanceClearing"
      @save="saveManualBalance"
      @clear="clearManualBalance"
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
