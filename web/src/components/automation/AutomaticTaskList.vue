<script setup lang="ts">
import { computed } from 'vue'
import {
  CheckmarkCircle24Regular,
  Clock24Regular,
  Delete24Regular,
  DismissCircle24Regular,
  Edit24Regular,
  History24Regular,
  Info24Regular,
  Play24Regular
} from '@vicons/fluent'
import type { AutomaticTask } from '../../types/automation'
import {
  automaticTaskEnvironmentLabel,
  automaticTaskNextRun,
  automaticTaskPayloadSummary,
  automaticTaskScheduleLabel,
  automaticTaskStatus,
  automaticTaskSummary,
  automaticTaskTypeLabel,
  formatAutomaticTaskDate
} from '../../utils/automaticTaskPresentation'

const props = defineProps<{
  tasks: AutomaticTask[]
  selectedTaskId: number | null
  loading: boolean
  loaded: boolean
  error: string
  runningTaskId: number | null
  togglingTaskId: number | null
  deletingTaskId: number | null
}>()

const emit = defineEmits<{
  select: [task: AutomaticTask]
  toggle: [task: AutomaticTask, enabled: boolean]
  run: [task: AutomaticTask]
  history: [task: AutomaticTask]
  edit: [task: AutomaticTask]
  delete: [task: AutomaticTask]
  refresh: []
}>()

const summary = computed(() => automaticTaskSummary(props.tasks))
const summaryNextRun = computed(() => summary.value.nextRunAt
  ? formatAutomaticTaskDate(summary.value.nextRunAt)
  : '暂无')

function actionBusy(taskId: number): boolean {
  return [props.runningTaskId, props.togglingTaskId, props.deletingTaskId].includes(taskId)
}

function selectFromKeyboard(event: KeyboardEvent, task: AutomaticTask) {
  if (!['Enter', ' '].includes(event.key)) return
  event.preventDefault()
  emit('select', task)
}
</script>

<template>
  <section class="task-list-region" aria-label="自动任务列表" :aria-busy="loading">
    <header class="task-summary" aria-label="自动任务摘要">
      <span><b>{{ summary.total }}</b> 个任务</span><i aria-hidden="true" />
      <span><b>{{ summary.enabled }}</b> 已启用</span><i aria-hidden="true" />
      <span><b>{{ summary.running }}</b> 执行中</span><i aria-hidden="true" />
      <span>下次运行 <strong>{{ summaryNextRun }}</strong></span>
    </header>

    <div v-if="error" class="task-state task-error" role="alert">
      <span>{{ error }}</span>
      <el-button text :disabled="loading" @click="emit('refresh')">重新读取</el-button>
    </div>
    <div v-if="loading && !loaded" class="task-state">正在读取自动任务</div>
    <div v-else-if="loaded && !tasks.length" class="task-state task-empty">
      <strong>暂无自动任务</strong>
      <span>使用页面右上角“新建任务”创建第一条计划</span>
    </div>
    <div v-else-if="tasks.length" class="task-table" role="table" aria-label="自动任务">
      <div class="task-row task-table-head" role="row">
        <span role="columnheader">任务</span>
        <span role="columnheader">设备 · eSIM 配置</span>
        <span role="columnheader">类型 · 环境</span>
        <span role="columnheader">计划 · 时区</span>
        <span role="columnheader">下次运行</span>
        <span role="columnheader">状态</span>
        <span role="columnheader">启用</span>
        <span role="columnheader">操作</span>
      </div>

      <article
        v-for="task in tasks"
        :key="task.id"
        class="task-row task-item"
        :class="{ selected: selectedTaskId === task.id }"
        role="row"
        tabindex="0"
        :aria-selected="selectedTaskId === task.id"
        @click="emit('select', task)"
        @keydown="selectFromKeyboard($event, task)"
      >
        <span class="task-cell task-identity" role="cell" data-label="任务">
          <b>{{ task.name }}</b>
          <small>{{ automaticTaskPayloadSummary(task) }}</small>
        </span>
        <span class="task-cell" role="cell" data-label="设备 · eSIM 配置">
          <b>{{ task.device_id || '未绑定设备' }}</b>
          <small class="task-code">{{ task.profile_iccid || '未绑定 SIM' }}</small>
        </span>
        <span class="task-cell" role="cell" data-label="类型 · 环境">
          <b>{{ automaticTaskTypeLabel(task) }}</b>
          <small>{{ automaticTaskEnvironmentLabel(task) }}</small>
        </span>
        <span class="task-cell" role="cell" data-label="计划 · 时区">
          <b class="task-code">{{ automaticTaskScheduleLabel(task) }}</b>
          <small>{{ task.timezone || '时区未提供' }}</small>
        </span>
        <span class="task-cell task-next-run" role="cell" data-label="下次运行">
          <b>{{ automaticTaskNextRun(task) }}</b>
        </span>
        <span class="task-cell" role="cell" data-label="状态">
          <span class="task-status" :class="`tone-${automaticTaskStatus(task.last_status).tone}`">
            <el-icon v-if="task.last_status === 'success'"><CheckmarkCircle24Regular /></el-icon>
            <el-icon v-else-if="task.last_status === 'failed'"><DismissCircle24Regular /></el-icon>
            <el-icon v-else-if="task.last_status === 'running'"><Clock24Regular /></el-icon>
            <el-icon v-else><Info24Regular /></el-icon>
            {{ automaticTaskStatus(task.last_status).label }}
          </span>
        </span>
        <span class="task-cell task-toggle" role="cell" data-label="启用">
          <el-switch
            :model-value="task.enabled"
            :loading="togglingTaskId === task.id"
            :disabled="actionBusy(task.id)"
            :aria-label="`${task.enabled ? '停用' : '启用'} ${task.name}`"
            @click.stop
            @change="emit('toggle', task, Boolean($event))"
          />
        </span>
        <span class="task-cell task-actions" role="cell" data-label="操作" @click.stop>
          <el-tooltip content="立即运行">
            <el-button
              circle text
              :loading="runningTaskId === task.id"
              :disabled="actionBusy(task.id)"
              :aria-label="`立即运行 ${task.name}`"
              @click="emit('run', task)"
            ><el-icon v-if="runningTaskId !== task.id"><Play24Regular /></el-icon></el-button>
          </el-tooltip>
          <el-tooltip content="运行记录"><el-button circle text :disabled="actionBusy(task.id)" :aria-label="`查看 ${task.name} 运行记录`" @click="emit('history', task)"><el-icon><History24Regular /></el-icon></el-button></el-tooltip>
          <el-tooltip content="编辑"><el-button circle text :disabled="actionBusy(task.id)" :aria-label="`编辑 ${task.name}`" @click="emit('edit', task)"><el-icon><Edit24Regular /></el-icon></el-button></el-tooltip>
          <el-tooltip content="删除"><el-button circle text type="danger" :loading="deletingTaskId === task.id" :disabled="actionBusy(task.id)" :aria-label="`删除 ${task.name}`" @click="emit('delete', task)"><el-icon v-if="deletingTaskId !== task.id"><Delete24Regular /></el-icon></el-button></el-tooltip>
        </span>
      </article>
    </div>
  </section>
</template>

<style scoped>
.task-list-region { min-width: 0; padding: 16px; }
.task-summary { min-height: 36px; padding: 0 4px 12px; display: flex; align-items: center; flex-wrap: wrap; gap: 10px; color: var(--ui-text-muted); font-size: var(--ui-font-body-sm); }
.task-summary span { white-space: nowrap; }
.task-summary b, .task-summary strong { color: var(--ui-text); font-weight: 550; }
.task-summary i { width: 3px; height: 3px; border-radius: 50%; background: var(--ui-text-subtle); }
.task-state { min-height: 220px; border: 1px solid var(--ui-border); border-radius: 6px; color: var(--ui-text-muted); display: grid; place-items: center; align-content: center; gap: 8px; font-size: var(--ui-font-body); text-align: center; }
.task-state strong { color: var(--ui-text); }
.task-error { min-height: 72px; margin-bottom: 10px; padding: 12px; border-color: color-mix(in srgb, var(--ui-danger) 38%, var(--ui-border)); color: var(--ui-danger); grid-template-columns: minmax(0, 1fr) auto; text-align: left; }
.task-table { min-width: 900px; border: 1px solid var(--ui-border); border-radius: 6px; overflow: hidden; }
.task-row { display: grid; grid-template-columns: minmax(130px, 1.15fr) minmax(130px, 1.05fr) minmax(105px, .85fr) minmax(132px, 1fr) minmax(118px, .9fr) minmax(90px, .7fr) 68px 176px; }
.task-table-head { min-height: 42px; align-items: center; border-bottom: 1px solid var(--ui-border); background: color-mix(in srgb, var(--ui-surface-muted) 40%, transparent); color: var(--ui-text-subtle); font-size: var(--ui-font-caption); }
.task-table-head > span { padding: 10px; }
.task-item { position: relative; min-height: 84px; border-bottom: 1px solid var(--ui-border); color: var(--ui-text-muted); cursor: pointer; transition: background-color 160ms ease, box-shadow 160ms ease, transform 160ms var(--ui-ease-out); }
.task-item:last-child { border-bottom: 0; }
.task-item::before { content: ''; position: absolute; inset: 0 auto 0 0; width: 2px; background: var(--ui-primary); opacity: 0; transform: scaleY(.45); transition: opacity 160ms var(--ui-ease-out), transform 200ms var(--ui-ease-out); }
.task-item.selected { background: color-mix(in srgb, var(--ui-primary) 8%, transparent); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--ui-primary) 45%, transparent); }
.task-item.selected::before { opacity: 1; transform: scaleY(1); }
.task-item:focus-visible { outline: 2px solid var(--ui-primary); outline-offset: -2px; }
.task-cell { min-width: 0; padding: 12px 10px; display: flex; flex-direction: column; justify-content: center; overflow: hidden; }
.task-cell b, .task-cell small { min-width: 0; display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.task-cell b { color: var(--ui-text); font-size: var(--ui-font-body-sm); font-weight: 550; }
.task-identity b { font-size: var(--ui-font-body); }
.task-cell small { margin-top: 4px; color: var(--ui-text-subtle); font-size: var(--ui-font-caption); }
.task-code { font-family: "v-mono", ui-monospace, monospace; }
.task-status { display: inline-flex; align-items: center; gap: 5px; font-size: var(--ui-font-body-sm); white-space: nowrap; }
.task-status .el-icon { font-size: 15px; }
.tone-neutral { color: var(--ui-text-muted); }
.tone-info { color: var(--ui-info); }
.tone-warning { color: var(--ui-warning); }
.tone-success { color: var(--ui-success); }
.tone-danger { color: var(--ui-danger); }
.task-toggle { align-items: center; }
.task-actions { padding-inline: 4px; flex-direction: row; align-items: center; gap: 0; }
.task-actions :deep(.el-button) { width: 40px; height: 40px; margin: 0; padding: 0; }
@media (hover: hover) and (pointer: fine) {
  .task-item:hover:not(.selected) { background: color-mix(in srgb, var(--ui-text) 2%, transparent); }
}
@media (max-width: 640px) {
  .task-list-region { padding: 12px 10px; }
  .task-summary { gap: 7px; padding-bottom: 14px; }
  .task-table { min-width: 0; border: 0; overflow: visible; }
  .task-table-head { display: none; }
  .task-item { min-height: 0; margin-bottom: 10px; padding: 12px; border: 1px solid var(--ui-border); border-radius: 6px; grid-template-columns: minmax(0, 1fr) auto; gap: 0 10px; }
  .task-item:last-child { border-bottom: 1px solid var(--ui-border); }
  .task-item.selected { border-color: color-mix(in srgb, var(--ui-primary) 68%, var(--ui-border)); box-shadow: none; }
  .task-cell { padding: 0; overflow: visible; }
  .task-cell::before { content: attr(data-label); margin-bottom: 3px; color: var(--ui-text-subtle); font-size: var(--ui-font-caption); }
  .task-identity { grid-column: 1; }
  .task-identity::before, .task-toggle::before, .task-actions::before { display: none; }
  .task-cell:nth-child(2), .task-cell:nth-child(3), .task-cell:nth-child(4), .task-cell:nth-child(5), .task-cell:nth-child(6) { grid-column: 1; margin-top: 12px; }
  .task-toggle { grid-column: 2; grid-row: 1; align-self: start; }
  .task-actions { grid-column: 2; grid-row: 2 / span 5; align-self: end; display: grid; grid-template-columns: 44px 44px; }
  .task-actions :deep(.el-button) { width: 44px; height: 44px; }
  .task-cell b, .task-cell small { white-space: normal; overflow-wrap: anywhere; }
}
@media (prefers-reduced-motion: reduce) {
  .task-item, .task-item::before { transition-duration: 120ms; transition-property: opacity, background-color, box-shadow; }
  .task-item::before { transform: none; }
}
</style>
