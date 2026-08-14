<script setup lang="ts">
import {
  ArrowSync24Regular,
  Call24Regular,
  Clock24Regular,
  Dismiss24Regular,
  DocumentBulletList24Regular,
  Edit24Regular,
  Mail24Regular,
  Phone24Regular,
  Sim24Regular,
  Wifi124Regular
} from '@vicons/fluent'
import type { AutomaticTask } from '../../types/automation'
import {
  automaticTaskEnvironmentLabel,
  automaticTaskPayloadSummary,
  automaticTaskTypeLabel
} from '../../utils/automaticTaskPresentation'

defineProps<{ task: AutomaticTask }>()

const emit = defineEmits<{
  close: []
  edit: [task: AutomaticTask]
  history: [task: AutomaticTask]
}>()
</script>

<template>
  <aside class="task-detail" aria-label="任务详情">
    <header>
      <div>
        <span>AUTOMATION PROFILE</span>
        <h2>{{ task.name }}</h2>
      </div>
      <el-button circle text aria-label="关闭任务详情" @click="emit('close')"><el-icon><Dismiss24Regular /></el-icon></el-button>
    </header>

    <section class="detail-panel">
      <h3>执行画像</h3>
      <dl>
        <div><dt><el-icon><DocumentBulletList24Regular /></el-icon>任务类型</dt><dd>{{ automaticTaskTypeLabel(task) }}</dd></div>
        <div><dt><el-icon><Wifi124Regular /></el-icon>运行环境</dt><dd>{{ automaticTaskEnvironmentLabel(task) }}</dd></div>
        <div><dt><el-icon><Phone24Regular /></el-icon>任务内容</dt><dd>{{ automaticTaskPayloadSummary(task) }}</dd></div>
        <div><dt><el-icon><Clock24Regular /></el-icon>保持时长</dt><dd>{{ task.task_type === 'call' ? `${task.payload.hold_seconds || '未提供'} 秒` : '不适用' }}</dd></div>
        <div><dt><el-icon><ArrowSync24Regular /></el-icon>失败重试</dt><dd>{{ task.retry_count }} 次</dd></div>
        <div><dt><el-icon><Mail24Regular /></el-icon>完成通知</dt><dd :class="task.notify ? 'enabled' : ''">{{ task.notify ? '已开启' : '已关闭' }}</dd></div>
      </dl>
    </section>

    <section class="detail-panel identity-panel">
      <h3>绑定目标</h3>
      <dl>
        <div><dt><el-icon><Call24Regular /></el-icon>设备</dt><dd>{{ task.device_id || '未绑定' }}</dd></div>
        <div><dt><el-icon><Sim24Regular /></el-icon>ICCID</dt><dd class="detail-code">{{ task.profile_iccid || '未绑定' }}</dd></div>
      </dl>
    </section>

    <footer>
      <el-button @click="emit('history', task)"><el-icon><Clock24Regular /></el-icon>运行记录</el-button>
      <el-button type="primary" plain @click="emit('edit', task)"><el-icon><Edit24Regular /></el-icon>编辑任务</el-button>
    </footer>
  </aside>
</template>

<style scoped>
.task-detail { min-width: 0; padding: 18px 16px; border-left: 1px solid var(--ui-border); background: color-mix(in srgb, var(--ui-surface-strong) 70%, transparent); overflow: auto; }
.task-detail > header { min-height: 42px; margin-bottom: 14px; padding: 0 3px; display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }
.task-detail > header span { color: var(--ui-primary); font: var(--ui-font-caption)/1.2 "v-mono", monospace; letter-spacing: .14em; }
.task-detail h2 { margin: 5px 0 0; color: var(--ui-text); font-size: 20px; overflow-wrap: anywhere; }
.task-detail > header :deep(.el-button) { width: 40px; height: 40px; flex: 0 0 40px; margin: -5px -5px 0 0; }
.detail-panel { padding: 14px; border: 1px solid var(--ui-border); border-radius: 6px; background: color-mix(in srgb, var(--ui-surface) 62%, transparent); }
.detail-panel + .detail-panel { margin-top: 10px; }
.detail-panel h3 { margin: 0 0 8px; color: var(--ui-text); font-size: 16px; }
.detail-panel dl { margin: 0; }
.detail-panel dl > div { min-height: 42px; display: grid; grid-template-columns: minmax(112px, 1fr) minmax(0, auto); align-items: center; gap: 10px; border-bottom: 1px solid var(--ui-border); }
.detail-panel dl > div:last-child { border-bottom: 0; }
.detail-panel dt { color: var(--ui-text-muted); display: flex; align-items: center; gap: 8px; font-size: var(--ui-font-body-sm); }
.detail-panel dt .el-icon { color: var(--ui-text-subtle); font-size: 17px; }
.detail-panel dd { min-width: 0; margin: 0; color: var(--ui-text); font-size: var(--ui-font-body-sm); font-weight: 550; text-align: right; overflow-wrap: anywhere; }
.detail-panel dd.enabled { color: var(--ui-success); }
.detail-code { font-family: "v-mono", ui-monospace, monospace; }
.task-detail > footer { margin-top: 12px; display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.task-detail > footer :deep(.el-button) { min-height: 40px; margin: 0; }
@media (max-width: 1180px) {
  .task-detail { border-top: 1px solid var(--ui-border); border-left: 0; display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
  .task-detail > header, .task-detail > footer { grid-column: 1 / -1; }
  .detail-panel + .detail-panel { margin-top: 0; }
}
@media (max-width: 640px) {
  .task-detail { padding: 14px 10px calc(92px + env(safe-area-inset-bottom)); display: block; }
  .detail-panel + .detail-panel { margin-top: 10px; }
  .task-detail > header :deep(.el-button), .task-detail > footer :deep(.el-button) { min-height: 44px; }
  .task-detail > footer { grid-template-columns: 1fr; }
}
</style>
