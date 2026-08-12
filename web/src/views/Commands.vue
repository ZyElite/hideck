<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import PageHeader from '../components/PageHeader.vue'
import CommandChat from '../components/commands/CommandChat.vue'
import BalanceDrawer from '../components/commands/BalanceDrawer.vue'
import RuleEditorDrawer from '../components/commands/RuleEditorDrawer.vue'
import { useEventStream } from '../composables/useEventStream'
import { commandService } from '../services/commands'
import { devicesService } from '../services/devices'
import type { DeviceMgmtListItem } from '../types/api'
import type { BalanceQuery, CarrierQueryRule, CommandDefinition, CommandEvent } from '../types/commands'
import { buildDangerousCommand } from '../utils/commandInput'

const pageSize = 100
const definitions = ref<CommandDefinition[]>([])
const events = ref<CommandEvent[]>([])
const balances = ref<BalanceQuery[]>([])
const devices = ref<DeviceMgmtListItem[]>([])
const builtInRules = ref<CarrierQueryRule[]>([])
const customRules = ref<CarrierQueryRule[]>([])
const selectedDevice = ref('')
const loading = ref(true)
const loadingOlder = ref(false)
const hasOlder = ref(false)
const executing = ref(false)
const querying = ref(false)
const rulesOpen = ref(false)
const balanceOpen = ref(false)
const savingRule = ref(false)
const dangerousDefinition = ref<CommandDefinition | null>(null)
const dangerForm = reactive({ device: '', target: '', phone: '', duration: 15 })
let balanceTimer: number | null = null
let disposed = false

const stream = useEventStream<CommandEvent>({
  path: '/command-center/stream',
  eventName: 'command',
  parse: (payload) => JSON.parse(payload) as CommandEvent,
  onEvent: (event) => mergeEvents([event]),
  reconnectDelayMs: 2500
})
const streamConnected = stream.connected
const selectedBalance = computed(() => balances.value.find((query) => query.device_id === selectedDevice.value))

const dangerousTitle = computed(() => {
  if (dangerousDefinition.value?.name === 'switch') return '切换 eSIM'
  if (dangerousDefinition.value?.name === 'vocall') return 'VoWiFi 通话'
  return '切换公网 IP'
})

onMounted(async () => {
  const pageData = Promise.all([loadCatalog(), loadDevices(), refreshBalances(), loadRules()])
  await loadEvents()
  if (disposed) return
  const latest = events.value.at(-1)?.id
  if (latest) stream.setLastEventId(latest)
  void stream.connect()
  await pageData
  if (disposed) return
  balanceTimer = window.setInterval(() => {
    if (balances.value.some((query) => query.state === 'sending' || query.state === 'awaiting_reply')) {
      void refreshBalances(true)
    }
  }, 5000)
  loading.value = false
})

onUnmounted(() => {
  disposed = true
  stream.disconnect()
  if (balanceTimer !== null) window.clearInterval(balanceTimer)
})

async function loadCatalog() {
  const result = await commandService.catalog()
  if (result.ok) definitions.value = result.data
  else ElMessage.error(result.error.message || '命令目录加载失败')
}

async function loadEvents() {
  const result = await commandService.events({ beforeId: 0, limit: pageSize })
  if (!result.ok) {
    ElMessage.error(result.error.message || '命令历史加载失败')
    return
  }
  events.value = result.data
  hasOlder.value = result.data.length === pageSize
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
  hasOlder.value = result.data.length === pageSize
}

async function loadDevices() {
  const result = await devicesService.listManaged()
  if (!result.ok) {
    ElMessage.error(result.error.message || '设备列表加载失败')
    return
  }
  devices.value = result.data.devices
  if (!selectedDevice.value && devices.value.length) selectedDevice.value = devices.value[0].id
}

async function refreshBalances(silent = false) {
  const result = await commandService.balances({ limit: 50 })
  if (result.ok) balances.value = result.data
  else if (!silent) ElMessage.error(result.error.message || '余额记录加载失败')
}

async function loadRules() {
  const result = await commandService.rules()
  if (!result.ok) {
    ElMessage.error(result.error.message || '运营商规则加载失败')
    return
  }
  builtInRules.value = result.data.builtIn
  customRules.value = result.data.custom
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
  if (input.trim().split(/\s+/, 1)[0]?.toLowerCase() === '/balance') await refreshBalances(true)
}

function mergeEvents(incoming: CommandEvent[]) {
  const merged = new Map(events.value.map((event) => [event.id, event]))
  for (const event of incoming) merged.set(event.id, event)
  events.value = [...merged.values()].sort((left, right) => left.id - right.id)
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

async function saveRule(rule: CarrierQueryRule) {
  savingRule.value = true
  const result = await commandService.saveRule(rule)
  savingRule.value = false
  if (!result.ok) {
    ElMessage.error(result.error.message || '规则保存失败')
    return
  }
  await loadRules()
  ElMessage.success('自定义规则已保存')
}

async function deleteRule(id: string) {
  const confirmed = await ElMessageBox.confirm(`删除自定义规则 ${id}？`, '删除规则', {
    confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning'
  }).then(() => true).catch(() => false)
  if (!confirmed) return
  const result = await commandService.deleteRule(id)
  if (!result.ok) {
    ElMessage.error(result.error.message || '规则删除失败')
    return
  }
  await loadRules()
  ElMessage.success('规则已删除')
}
</script>

<template>
  <div class="app-page commands-page">
    <PageHeader title="命令中心" subtitle="通过设备会话执行查询、短信与 VoWiFi 操作" />

    <div class="commands-layout">
      <CommandChat
        v-model:selected-device="selectedDevice"
        :events="events"
        :balance-queries="balances"
        :latest-balance="selectedBalance"
        :definitions="definitions"
        :devices="devices"
        :loading="loading"
        :loading-older="loadingOlder"
        :has-older="hasOlder"
        :busy="executing"
        :stream-connected="streamConnected"
        @load-older="loadOlder"
        @clear-history="clearHistory"
        @open-balance="balanceOpen = true"
        @submit="execute"
        @dangerous="openDangerous"
      />
    </div>

    <BalanceDrawer
      v-model="balanceOpen"
      v-model:selected-device="selectedDevice"
      :devices="devices"
      :queries="balances"
      :loading="loading"
      :querying="querying"
      @query="startBalance"
      @edit-rules="rulesOpen = true"
    />

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
      @save="saveRule"
      @delete="deleteRule"
    />
  </div>
</template>

<style scoped>
.commands-page { min-width: 0; }
.commands-layout { height: calc(100dvh - 166px); min-height: 520px; }
@media (max-width: 1023px) {
  .commands-layout { height: calc(100dvh - 166px); min-height: 520px; }
}
@media (max-width: 640px) {
  .commands-layout { height: calc(100dvh - 178px); min-height: 480px; margin: 0 -4px; }
}
</style>
