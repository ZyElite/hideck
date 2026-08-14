<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
  ArrowClockwise24Regular,
  CheckmarkCircle24Regular,
  Clock24Regular,
  Dismiss24Regular,
  DismissCircle24Regular,
  History24Regular,
  Info24Regular
} from '@vicons/fluent'
import { automationService } from '../services/automation'
import type { AutomaticTask, AutomaticTaskRun } from '../types/automation'
import {
  automaticTaskRunResult,
  automaticTaskStatus,
  formatAutomaticTaskDate
} from '../utils/automaticTaskPresentation'

const RUN_REFRESH_INTERVAL_MS = 3_000
const PAGE_SIZE = 20

const props = defineProps<{
  modelValue: boolean
  task: AutomaticTask | null
}>()

const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()
const runs = ref<AutomaticTaskRun[]>([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const loaded = ref(false)
const runsError = ref('')
let refreshTimer: number | null = null
let requestGeneration = 0

watch([() => props.modelValue, () => props.task?.id], ([open]) => {
  clearRefresh()
  if (!open || !props.task) return
  page.value = 1
  runs.value = []
  total.value = 0
  loaded.value = false
  runsError.value = ''
  void loadRuns(false)
  refreshTimer = window.setInterval(() => void loadRuns(true), RUN_REFRESH_INTERVAL_MS)
})

onUnmounted(clearRefresh)

async function loadRuns(silent = false) {
  const taskID = props.task?.id
  if (!taskID) return
  const requestID = ++requestGeneration
  if (!silent) loading.value = true
  const result = await automationService.runs({
    taskId: taskID,
    limit: PAGE_SIZE,
    offset: (page.value - 1) * PAGE_SIZE
  })
  if (requestID !== requestGeneration || props.task?.id !== taskID) return
  loading.value = false
  loaded.value = true
  if (!result.ok) {
    runsError.value = result.error.message || '运行记录加载失败'
    if (!silent) ElMessage.error(runsError.value)
    return
  }
  runsError.value = ''
  runs.value = result.data.runs
  total.value = result.data.total
}

function clearRefresh() {
  requestGeneration++
  loading.value = false
  if (refreshTimer !== null) window.clearInterval(refreshTimer)
  refreshTimer = null
}

function handleOpenChange(open: boolean) {
  emit('update:modelValue', open)
}
</script>

<template>
  <el-drawer
    :model-value="modelValue"
    class="automatic-task-runs"
    modal-class="automatic-task-runs-scrim"
    direction="rtl"
    size="min(620px, 100vw)"
    append-to-body
    destroy-on-close
    :show-close="false"
    @update:model-value="handleOpenChange"
  >
    <template #header>
      <header class="runs-header">
        <span class="runs-icon" aria-hidden="true"><el-icon><History24Regular /></el-icon></span>
        <div>
          <span>EXECUTION HISTORY</span>
          <h2>{{ task ? `${task.name} · 运行记录` : '运行记录' }}</h2>
        </div>
        <el-button circle text aria-label="关闭运行记录" @click="handleOpenChange(false)"><el-icon><Dismiss24Regular /></el-icon></el-button>
      </header>
    </template>

    <section class="runs-toolbar" aria-label="运行记录摘要">
      <div><strong>{{ loaded ? total : '—' }}</strong><span>条真实执行记录</span></div>
      <el-button :loading="loading" :disabled="loading || !task" @click="loadRuns(false)">
        <el-icon v-if="!loading"><ArrowClockwise24Regular /></el-icon>刷新
      </el-button>
    </section>

    <div v-if="runsError" class="runs-error" role="alert">
      <span>{{ runsError }}</span>
      <el-button text :disabled="loading" @click="loadRuns(false)">重新读取</el-button>
    </div>
    <div v-if="loading && !loaded" class="runs-state">正在读取运行记录</div>
    <div v-else-if="loaded && !runs.length" class="runs-state">
      <strong>暂无运行记录</strong>
      <span>该任务尚未产生可显示的执行结果</span>
    </div>
    <div v-else-if="runs.length" class="run-timeline" aria-live="polite">
      <article v-for="run in runs" :key="run.id" class="run-item" :class="`tone-${automaticTaskStatus(run.status).tone}`">
        <span class="run-marker" aria-hidden="true">
          <el-icon v-if="run.status === 'success'"><CheckmarkCircle24Regular /></el-icon>
          <el-icon v-else-if="run.status === 'failed'"><DismissCircle24Regular /></el-icon>
          <el-icon v-else-if="run.status === 'running'"><Clock24Regular /></el-icon>
          <el-icon v-else><Info24Regular /></el-icon>
        </span>
        <div class="run-content">
          <header>
            <time>{{ formatAutomaticTaskDate(run.scheduled_at) }}</time>
            <strong>{{ automaticTaskStatus(run.status).label }}</strong>
          </header>
          <p :class="{ 'run-error-text': run.error }">{{ automaticTaskRunResult(run) }}</p>
          <dl>
            <div><dt>尝试</dt><dd>{{ run.attempts }}</dd></div>
            <div><dt>开始</dt><dd>{{ formatAutomaticTaskDate(run.started_at) }}</dd></div>
            <div><dt>完成</dt><dd>{{ formatAutomaticTaskDate(run.finished_at) }}</dd></div>
          </dl>
        </div>
      </article>
    </div>

    <div v-if="total > PAGE_SIZE" class="runs-pagination">
      <el-pagination
        v-model:current-page="page"
        :page-size="PAGE_SIZE"
        :total="total"
        :pager-count="5"
        layout="prev, pager, next"
        @current-change="loadRuns(false)"
      />
    </div>
  </el-drawer>
</template>

<style scoped>
:global(.automatic-task-runs-scrim) { background: color-mix(in srgb, #000 54%, transparent); }
:global(.automatic-task-runs) { border-radius: 8px 0 0 8px; background: var(--ui-surface); }
:global(.automatic-task-runs .el-drawer__header) { min-height: 70px; margin: 0; padding: 12px 18px; border-bottom: 1px solid var(--ui-border); color: var(--ui-text); }
:global(.automatic-task-runs .el-drawer__body) { min-height: 0; padding: 0 18px 24px; overflow-y: auto; }
.runs-header { width: 100%; min-width: 0; display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 10px; }
.runs-icon { width: 38px; height: 38px; border: 1px solid color-mix(in srgb, var(--ui-primary) 42%, var(--ui-border)); border-radius: 6px; background: color-mix(in srgb, var(--ui-primary) 9%, transparent); color: var(--ui-primary); display: grid; place-items: center; }
.runs-header div > span { color: var(--ui-primary); font: var(--ui-font-caption)/1.2 "v-mono", monospace; letter-spacing: .14em; }
.runs-header h2 { margin: 4px 0 0; color: var(--ui-text); font-size: 18px; overflow-wrap: anywhere; }
.runs-header :deep(.el-button) { width: 44px; height: 44px; }
.runs-toolbar { min-height: 68px; border-bottom: 1px solid var(--ui-border); display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.runs-toolbar strong, .runs-toolbar span { display: block; }
.runs-toolbar strong { color: var(--ui-text); font-size: 18px; }
.runs-toolbar span { margin-top: 2px; color: var(--ui-text-subtle); font-size: var(--ui-font-caption); }
.runs-toolbar :deep(.el-button) { min-height: 40px; }
.runs-error { margin-top: 12px; padding: 10px 12px; border: 1px solid color-mix(in srgb, var(--ui-danger) 40%, var(--ui-border)); border-radius: 4px; background: color-mix(in srgb, var(--ui-danger) 7%, transparent); color: var(--ui-danger); display: flex; align-items: center; justify-content: space-between; gap: 10px; font-size: var(--ui-font-body-sm); }
.runs-state { min-height: 220px; color: var(--ui-text-muted); display: grid; place-items: center; align-content: center; gap: 7px; text-align: center; }
.runs-state strong { color: var(--ui-text); }
.runs-state span { font-size: var(--ui-font-body-sm); }
.run-timeline { padding-top: 8px; }
.run-item { position: relative; min-width: 0; padding: 13px 0 13px 42px; color: var(--ui-text-muted); }
.run-item::before { content: ''; position: absolute; top: 38px; bottom: -8px; left: 15px; width: 1px; background: var(--ui-border); }
.run-item:last-child::before { display: none; }
.run-marker { position: absolute; top: 15px; left: 2px; z-index: 1; width: 28px; height: 28px; border: 1px solid currentColor; border-radius: 50%; background: var(--ui-surface-strong); display: grid; place-items: center; }
.run-marker .el-icon { font-size: 15px; }
.tone-info { color: var(--ui-info); }
.tone-warning { color: var(--ui-warning); }
.tone-success { color: var(--ui-success); }
.tone-danger { color: var(--ui-danger); }
.run-content { min-width: 0; padding-bottom: 13px; border-bottom: 1px solid var(--ui-border); }
.run-content > header { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.run-content time { color: var(--ui-text-muted); font: var(--ui-font-body-sm) "v-mono", ui-monospace, monospace; }
.run-content > header strong { color: currentColor; font-size: var(--ui-font-body-sm); }
.run-content p { margin: 8px 0; color: var(--ui-text); font-size: var(--ui-font-body); line-height: 1.5; white-space: pre-wrap; overflow-wrap: anywhere; }
.run-content p.run-error-text { color: var(--ui-danger); }
.run-content dl { margin: 0; display: grid; grid-template-columns: auto 1fr auto 1fr auto 1fr; gap: 5px 8px; color: var(--ui-text-subtle); font-size: var(--ui-font-caption); }
.run-content dl div { display: contents; }
.run-content dd { min-width: 0; margin: 0; color: var(--ui-text-muted); overflow-wrap: anywhere; }
.runs-pagination { padding-top: 16px; display: flex; justify-content: flex-end; }
@media (max-width: 640px) {
  :global(.automatic-task-runs) { border-radius: 0; }
  :global(.automatic-task-runs .el-drawer__body) { padding: 0 14px calc(88px + env(safe-area-inset-bottom)); }
  .runs-toolbar :deep(.el-button) { min-height: 44px; }
  .run-content dl { grid-template-columns: auto minmax(0, 1fr); }
  .runs-pagination { justify-content: center; }
  .runs-pagination :deep(.btn-prev),
  .runs-pagination :deep(.btn-next),
  .runs-pagination :deep(.el-pager li) { min-width: 44px; height: 44px; }
}
@media (prefers-reduced-motion: reduce) {
  :global(.automatic-task-runs), :global(.automatic-task-runs-scrim) { animation: none !important; transition-duration: 120ms !important; }
}
</style>
