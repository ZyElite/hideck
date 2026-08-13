<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Add24Regular,
  ArrowClockwise24Regular,
  Delete24Regular,
  Edit24Regular,
  History24Regular,
  Play24Regular
} from '@vicons/fluent'
import AutomaticTaskDialog from '../components/AutomaticTaskDialog.vue'
import AutomaticTaskRunsDialog from '../components/AutomaticTaskRunsDialog.vue'
import { automationService } from '../services/automation'
import { devicesService } from '../services/devices'
import type { DeviceMgmtListItem } from '../types/api'
import type { AutomaticTask, AutomaticTaskInput, AutomaticTaskRunStatus } from '../types/automation'

const tasks = ref<AutomaticTask[]>([])
const devices = ref<DeviceMgmtListItem[]>([])
const loading = ref(true)
const saving = ref(false)
const dialogOpen = ref(false)
const historyOpen = ref(false)
const editingTask = ref<AutomaticTask | null>(null)
const historyTask = ref<AutomaticTask | null>(null)
const runningTask = ref<number | null>(null)
const togglingTask = ref<number | null>(null)
let refreshTimer: number | null = null

onMounted(async () => {
  await Promise.all([loadTasks(), loadDevices()])
  loading.value = false
  refreshTimer = window.setInterval(() => void loadTasks(true), 10_000)
})

onUnmounted(() => {
  if (refreshTimer !== null) window.clearInterval(refreshTimer)
})

async function loadTasks(silent = false) {
  if (!silent) loading.value = true
  const result = await automationService.list()
  if (!silent) loading.value = false
  if (!result.ok) {
    if (!silent) ElMessage.error(result.error.message || '自动任务加载失败')
    return
  }
  tasks.value = result.data
}

async function loadDevices() {
  const result = await devicesService.listManaged()
  if (!result.ok) {
    ElMessage.error(result.error.message || '设备列表加载失败')
    return
  }
  devices.value = result.data.devices
}

function openCreate() {
  editingTask.value = null
  dialogOpen.value = true
}

function openEdit(task: AutomaticTask) {
  editingTask.value = task
  dialogOpen.value = true
}

async function saveTask(input: AutomaticTaskInput) {
  saving.value = true
  const result = editingTask.value
    ? await automationService.update(editingTask.value.id, input)
    : await automationService.create(input)
  saving.value = false
  if (!result.ok) {
    ElMessage.error(result.error.message || '自动任务保存失败')
    return
  }
  dialogOpen.value = false
  ElMessage.success(editingTask.value ? '自动任务已更新' : '自动任务已创建')
  await loadTasks(true)
}

async function toggleTask(task: AutomaticTask) {
  togglingTask.value = task.id
  const enabled = task.enabled
  const result = await automationService.update(task.id, taskToInput(task))
  togglingTask.value = null
  if (!result.ok) {
    task.enabled = !enabled
    ElMessage.error(result.error.message || '任务状态更新失败')
    return
  }
  Object.assign(task, result.data)
}

async function runNow(task: AutomaticTask) {
  runningTask.value = task.id
  const result = await automationService.runNow(task.id)
  runningTask.value = null
  if (!result.ok) {
    ElMessage.error(result.error.message || '任务排队失败')
    return
  }
  ElMessage.success('任务已进入设备执行队列')
  openHistory(task)
}

async function removeTask(task: AutomaticTask) {
  const confirmed = await ElMessageBox.confirm(`删除自动任务“${task.name}”？运行记录也会一并删除。`, '删除自动任务', {
    confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning'
  }).then(() => true).catch(() => false)
  if (!confirmed) return
  const result = await automationService.remove(task.id)
  if (!result.ok) {
    ElMessage.error(result.error.message || '自动任务删除失败')
    return
  }
  tasks.value = tasks.value.filter((item) => item.id !== task.id)
  ElMessage.success('自动任务已删除')
}

function openHistory(task: AutomaticTask) {
  historyTask.value = task
  historyOpen.value = true
}

function taskToInput(task: AutomaticTask): AutomaticTaskInput {
  return {
    name: task.name, enabled: task.enabled, device_id: task.device_id,
    profile_iccid: task.profile_iccid, profile_aid: task.profile_aid,
    task_type: task.task_type, environment: task.environment,
    interval_days: task.interval_days, start_date: task.start_date,
    run_time: task.run_time, timezone: task.timezone,
    payload: { ...task.payload }, retry_count: task.retry_count, notify: task.notify
  }
}

function taskTypeText(task: AutomaticTask) {
  return { sms: '短信', call: '通话', public_ip: '公网 IP' }[task.task_type]
}

function statusType(status?: AutomaticTaskRunStatus) {
  if (status === 'success') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'running') return 'warning'
  return 'info'
}

function statusText(status?: AutomaticTaskRunStatus) {
  if (!status) return '未运行'
  return { queued: '排队中', running: '执行中', success: '成功', failed: '失败' }[status]
}

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString() : '—'
}
</script>

<template>
  <div class="app-page automation-page">
    <header class="page-heading">
      <div>
        <h1>自动任务</h1>
      </div>
      <div class="heading-actions">
        <el-button circle text :loading="loading" aria-label="刷新自动任务" @click="loadTasks()">
          <el-icon v-if="!loading"><ArrowClockwise24Regular /></el-icon>
        </el-button>
        <el-button type="primary" @click="openCreate">
          <el-icon><Add24Regular /></el-icon>
          新建任务
        </el-button>
      </div>
    </header>

    <section class="ui-panel task-table-panel">
      <el-table :data="tasks" v-loading="loading" stripe class="w-full">
        <el-table-column label="任务" min-width="190">
          <template #default="{ row }">
            <div class="task-name">{{ row.name }}</div>
            <div class="task-meta">{{ row.device_id }} · {{ row.profile_iccid }}</div>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="120">
          <template #default="{ row }">
            <div>{{ taskTypeText(row) }}</div>
            <div class="task-meta">{{ row.environment === 'vowifi' ? 'VoWiFi' : '蜂窝' }}</div>
          </template>
        </el-table-column>
        <el-table-column label="周期" min-width="170">
          <template #default="{ row }">
            <div>每 {{ row.interval_days }} 天 · {{ row.run_time }}</div>
            <div class="task-meta">{{ row.timezone }}</div>
          </template>
        </el-table-column>
        <el-table-column label="下次运行" min-width="170">
          <template #default="{ row }">{{ row.enabled ? formatDate(row.next_run_at) : '已停用' }}</template>
        </el-table-column>
        <el-table-column label="上次状态" width="104">
          <template #default="{ row }">
            <el-tooltip :content="row.last_error || statusText(row.last_status)" placement="top">
              <el-tag :type="statusType(row.last_status)" effect="plain">{{ statusText(row.last_status) }}</el-tag>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="启用" width="76">
          <template #default="{ row }">
            <el-switch v-model="row.enabled" :loading="togglingTask === row.id" @change="toggleTask(row)" />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <div class="row-actions">
              <el-tooltip content="立即运行"><el-button circle text :loading="runningTask === row.id" @click="runNow(row)"><el-icon v-if="runningTask !== row.id"><Play24Regular /></el-icon></el-button></el-tooltip>
              <el-tooltip content="运行记录"><el-button circle text @click="openHistory(row)"><el-icon><History24Regular /></el-icon></el-button></el-tooltip>
              <el-tooltip content="编辑"><el-button circle text @click="openEdit(row)"><el-icon><Edit24Regular /></el-icon></el-button></el-tooltip>
              <el-tooltip content="删除"><el-button circle text type="danger" @click="removeTask(row)"><el-icon><Delete24Regular /></el-icon></el-button></el-tooltip>
            </div>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无自动任务" />
        </template>
      </el-table>
    </section>

    <AutomaticTaskDialog
      v-model="dialogOpen"
      :task="editingTask"
      :devices="devices"
      :saving="saving"
      @submit="saveTask"
    />
    <AutomaticTaskRunsDialog v-model="historyOpen" :task="historyTask" />
  </div>
</template>

<style scoped>
.automation-page { padding-top: 22px; }
.page-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 18px; }
.page-heading h1 { margin: 0; font-size: 24px; line-height: 1.25; letter-spacing: 0; }
.heading-actions, .row-actions { display: flex; align-items: center; gap: 6px; }
.task-table-panel { overflow: hidden; }
.task-name { font-weight: 650; color: var(--ui-text); }
.task-meta { margin-top: 3px; color: var(--ui-text-muted); font-size: 12px; font-family: "v-mono", ui-monospace, monospace; }
@media (max-width: 640px) {
  .page-heading { align-items: stretch; flex-direction: column; }
  .heading-actions { justify-content: flex-end; }
}
</style>
