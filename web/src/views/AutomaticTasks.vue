<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Add24Regular, ArrowClockwise24Regular } from '@vicons/fluent'
import AutomaticTaskDialog from '../components/AutomaticTaskDialog.vue'
import AutomaticTaskRunsDialog from '../components/AutomaticTaskRunsDialog.vue'
import AutomaticTaskDetail from '../components/automation/AutomaticTaskDetail.vue'
import AutomaticTaskList from '../components/automation/AutomaticTaskList.vue'
import { automationService } from '../services/automation'
import { devicesService } from '../services/devices'
import type { DeviceMgmtListItem } from '../types/api'
import type { AutomaticTask, AutomaticTaskInput } from '../types/automation'

const TASK_REFRESH_INTERVAL_MS = 10_000

const tasks = ref<AutomaticTask[]>([])
const devices = ref<DeviceMgmtListItem[]>([])
const loading = ref(true)
const tasksLoaded = ref(false)
const tasksError = ref('')
const saving = ref(false)
const dialogOpen = ref(false)
const historyOpen = ref(false)
const editingTask = ref<AutomaticTask | null>(null)
const historyTask = ref<AutomaticTask | null>(null)
const selectedTaskId = ref<number | null>(null)
const detailDismissed = ref(false)
const runningTaskId = ref<number | null>(null)
const togglingTaskId = ref<number | null>(null)
const deletingTaskId = ref<number | null>(null)
let refreshTimer: number | null = null

const selectedTask = computed(() => tasks.value.find((task) => task.id === selectedTaskId.value) || null)

onMounted(async () => {
  await Promise.all([loadTasks(), loadDevices()])
  refreshTimer = window.setInterval(() => void loadTasks(true), TASK_REFRESH_INTERVAL_MS)
})

onUnmounted(() => {
  if (refreshTimer !== null) window.clearInterval(refreshTimer)
})

async function loadTasks(silent = false): Promise<boolean> {
  if (!silent) loading.value = true
  const result = await automationService.list()
  if (!silent) loading.value = false
  if (!result.ok) {
    tasksError.value = result.error.message || '自动任务加载失败'
    if (!silent) ElMessage.error(tasksError.value)
    return false
  }
  tasks.value = result.data
  tasksLoaded.value = true
  tasksError.value = ''
  syncSelection()
  return true
}

async function loadDevices() {
  const result = await devicesService.listManaged()
  if (!result.ok) {
    ElMessage.error(result.error.message || '设备列表加载失败')
    return
  }
  devices.value = result.data.devices
}

function syncSelection() {
  if (selectedTaskId.value && tasks.value.some((task) => task.id === selectedTaskId.value)) return
  if (detailDismissed.value) {
    selectedTaskId.value = null
    return
  }
  selectedTaskId.value = tasks.value[0]?.id || null
}

function selectTask(task: AutomaticTask) {
  detailDismissed.value = false
  selectedTaskId.value = task.id
}

function closeDetail() {
  detailDismissed.value = true
  selectedTaskId.value = null
}

function openCreate() {
  editingTask.value = null
  dialogOpen.value = true
}

function openEdit(task: AutomaticTask) {
  selectTask(task)
  editingTask.value = task
  dialogOpen.value = true
}

async function saveTask(input: AutomaticTaskInput) {
  if (saving.value) return
  saving.value = true
  const updatingTask = editingTask.value
  const result = updatingTask
    ? await automationService.update(updatingTask.id, input)
    : await automationService.create(input)
  saving.value = false
  if (!result.ok) {
    ElMessage.error(result.error.message || '自动任务保存失败')
    return
  }
  dialogOpen.value = false
  selectedTaskId.value = result.data.id
  ElMessage.success(updatingTask ? '自动任务已更新' : '自动任务已创建')
  await loadTasks(true)
}

async function toggleTask(task: AutomaticTask, enabled: boolean) {
  if (rowBusy(task.id)) return
  togglingTaskId.value = task.id
  const result = await automationService.update(task.id, taskToInput(task, { enabled }))
  togglingTaskId.value = null
  if (!result.ok) {
    ElMessage.error(result.error.message || '任务状态更新失败')
    return
  }
  Object.assign(task, result.data)
}

async function runNow(task: AutomaticTask) {
  if (rowBusy(task.id)) return
  runningTaskId.value = task.id
  const result = await automationService.runNow(task.id)
  runningTaskId.value = null
  if (!result.ok) {
    ElMessage.error(result.error.message || '任务排队失败')
    return
  }
  ElMessage.success('任务已进入设备执行队列')
  openHistory(task)
}

async function removeTask(task: AutomaticTask) {
  if (rowBusy(task.id)) return
  const confirmed = await ElMessageBox.confirm(`删除自动任务“${task.name}”？运行记录也会一并删除。`, '删除自动任务', {
    confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning'
  }).then(() => true).catch(() => false)
  if (!confirmed) return
  deletingTaskId.value = task.id
  const result = await automationService.remove(task.id)
  deletingTaskId.value = null
  if (!result.ok) {
    ElMessage.error(result.error.message || '自动任务删除失败')
    return
  }
  tasks.value = tasks.value.filter((item) => item.id !== task.id)
  syncSelection()
  ElMessage.success('自动任务已删除')
}

function openHistory(task: AutomaticTask) {
  selectTask(task)
  historyTask.value = task
  historyOpen.value = true
}

function rowBusy(taskId: number): boolean {
  return [runningTaskId.value, togglingTaskId.value, deletingTaskId.value].includes(taskId)
}

function taskToInput(task: AutomaticTask, overrides: Partial<AutomaticTaskInput> = {}): AutomaticTaskInput {
  return {
    name: task.name, enabled: task.enabled, device_id: task.device_id,
    profile_iccid: task.profile_iccid, profile_aid: task.profile_aid,
    task_type: task.task_type, environment: task.environment,
    interval_days: task.interval_days, start_date: task.start_date,
    run_time: task.run_time, timezone: task.timezone,
    payload: { ...task.payload }, retry_count: task.retry_count, notify: task.notify,
    ...overrides
  }
}
</script>

<template>
  <div class="app-page automation-page">
    <header class="page-heading">
      <div>
        <span>VOHIVE / AUTOMATION</span>
        <h1>自动任务</h1>
        <p>定时执行短信、通话与公网 IP 任务</p>
      </div>
      <div class="heading-actions">
        <el-button :loading="loading" :disabled="loading" @click="loadTasks()">
          <el-icon v-if="!loading"><ArrowClockwise24Regular /></el-icon>刷新
        </el-button>
        <el-button type="primary" @click="openCreate">
          <el-icon><Add24Regular /></el-icon>新建任务
        </el-button>
      </div>
    </header>

    <section class="automation-shell ui-card" :class="{ 'detail-open': selectedTask }">
      <AutomaticTaskList
        :tasks="tasks"
        :selected-task-id="selectedTaskId"
        :loading="loading"
        :loaded="tasksLoaded"
        :error="tasksError"
        :running-task-id="runningTaskId"
        :toggling-task-id="togglingTaskId"
        :deleting-task-id="deletingTaskId"
        @select="selectTask"
        @toggle="toggleTask"
        @run="runNow"
        @history="openHistory"
        @edit="openEdit"
        @delete="removeTask"
        @refresh="loadTasks()"
      />
      <Transition name="task-detail">
        <AutomaticTaskDetail
          v-if="selectedTask"
          :key="selectedTask.id"
          :task="selectedTask"
          @close="closeDetail"
          @edit="openEdit"
          @history="openHistory"
        />
      </Transition>
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
.automation-page { min-width: 0; padding-top: 22px; }
.page-heading { margin-bottom: 18px; display: flex; align-items: flex-end; justify-content: space-between; gap: 20px; }
.page-heading > div:first-child > span { color: var(--ui-primary); font: var(--ui-font-caption)/1.2 "v-mono", monospace; letter-spacing: .16em; }
.page-heading h1 { margin: 6px 0 2px; color: var(--ui-text); font-size: 24px; line-height: 1.2; }
.page-heading p { margin: 0; color: var(--ui-text-muted); font-size: var(--ui-font-body-sm); }
.heading-actions { display: flex; align-items: center; gap: 8px; }
.heading-actions :deep(.el-button) { min-height: 40px; margin: 0; }
.automation-shell { min-width: 0; overflow: hidden; display: grid; grid-template-columns: minmax(0, 1fr); }
.automation-shell.detail-open { grid-template-columns: minmax(0, 1fr) clamp(320px, 26vw, 400px); }
.task-list-region { overflow-x: auto; }
.task-detail-enter-active { transition: opacity 220ms var(--ui-ease-out), transform 220ms var(--ui-ease-out); }
.task-detail-leave-active { transition: opacity 120ms var(--ui-ease-out), transform 120ms var(--ui-ease-out); }
.task-detail-enter-from, .task-detail-leave-to { opacity: 0; transform: translateX(12px); }
@media (max-width: 1180px) {
  .automation-shell, .automation-shell.detail-open { display: grid; grid-template-columns: minmax(0, 1fr); }
}
@media (max-width: 820px) {
  .automation-page { padding-inline: 12px; padding-bottom: calc(92px + env(safe-area-inset-bottom)); }
}
@media (max-width: 640px) {
  .page-heading { align-items: flex-start; flex-direction: column; gap: 14px; }
  .page-heading h1 { font-size: 20px; }
  .heading-actions { width: 100%; justify-content: flex-start; }
  .heading-actions :deep(.el-button) { min-height: 44px; }
  .task-list-region { overflow: visible; }
}
@media (prefers-reduced-motion: reduce) {
  .task-detail-enter-active, .task-detail-leave-active { transition: opacity 120ms ease; }
  .task-detail-enter-from, .task-detail-leave-to { transform: none; }
}
</style>
