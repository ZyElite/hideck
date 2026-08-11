<script setup lang="ts">
import type { CommandEvent } from '../../types/commands'
import { formatDeviceDateTime } from '../../utils/deviceTime'
import { Bot24Regular, History24Regular } from '@vicons/fluent'

defineProps<{
  events: CommandEvent[]
  loading: boolean
  hasOlder: boolean
}>()

defineEmits<{ loadOlder: [] }>()

function eventLabel(event: CommandEvent) {
  if (event.kind === 'accepted') return '你'
  if (event.kind === 'progress') return '处理中'
  if (event.kind === 'error') return '执行失败'
  return 'VoHive'
}

function eventText(event: CommandEvent) {
  if (event.kind === 'accepted' && event.execution?.input) return event.execution.input
  return event.text
}
</script>

<template>
  <section class="timeline" aria-label="命令对话记录">
    <div class="timeline-toolbar">
      <div>
        <h3>执行时间线</h3>
        <p>{{ events.length }} 条事件</p>
      </div>
      <el-button v-if="hasOlder" text :loading="loading" @click="$emit('loadOlder')">
        <el-icon><History24Regular /></el-icon>
        更早记录
      </el-button>
    </div>

    <div class="timeline-scroll" aria-live="polite">
      <div v-if="loading && !events.length" class="empty-line">正在读取命令记录</div>
      <div v-else-if="!events.length" class="empty-state">
        <el-icon><Bot24Regular /></el-icon>
        <strong>暂无命令记录</strong>
      </div>
      <article
        v-for="event in events"
        :key="event.id"
        class="message"
        :class="{ user: event.kind === 'accepted', error: event.kind === 'error' }"
      >
        <div class="message-meta">
          <span>{{ eventLabel(event) }}</span>
          <time>{{ formatDeviceDateTime(event.created_at) }}</time>
        </div>
        <pre>{{ eventText(event) }}</pre>
        <span v-if="event.execution?.state === 'running'" class="state running">执行中</span>
        <span v-else-if="event.execution?.state === 'failed'" class="state failed">失败</span>
      </article>
    </div>
  </section>
</template>

<style scoped>
.timeline { min-height: 0; display: flex; flex-direction: column; }
.timeline-toolbar { min-height: 58px; padding: 10px 14px; border-bottom: 1px solid var(--ui-border); display: flex; align-items: center; justify-content: space-between; }
.timeline-toolbar h3 { margin: 0; font-size: 15px; font-weight: 700; letter-spacing: 0; }
.timeline-toolbar p { margin: 2px 0 0; color: #94a3b8; font-size: 12px; }
.timeline-scroll { min-height: 0; overflow: auto; padding: 16px; display: flex; flex-direction: column; gap: 12px; }
.message { max-width: min(82%, 720px); align-self: flex-start; }
.message.user { align-self: flex-end; }
.message-meta { margin: 0 3px 5px; display: flex; align-items: center; gap: 10px; color: #64748b; font-size: 11px; }
.message.user .message-meta { justify-content: flex-end; }
.message pre { margin: 0; padding: 11px 13px; border: 1px solid var(--ui-border); border-radius: 8px; background: var(--ui-surface-strong); color: inherit; font: 13px/1.55 "v-mono", ui-monospace, monospace; white-space: pre-wrap; overflow-wrap: anywhere; }
.message.user pre { background: #0f766e; border-color: #0f766e; color: white; }
.message.error pre { border-color: rgba(239, 68, 68, .35); background: rgba(239, 68, 68, .07); }
.state { display: inline-block; margin: 5px 3px 0; font-size: 11px; color: #64748b; }
.state.running { color: #0284c7; }
.state.failed { color: #dc2626; }
.empty-state, .empty-line { min-height: 260px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px; color: #94a3b8; text-align: center; }
.empty-state .el-icon { font-size: 30px; color: #0d9488; }
.empty-state strong { color: #475569; font-size: 14px; }
:global(html.dark) .empty-state strong { color: #cbd5e1; }
@media (max-width: 640px) { .message { max-width: 94%; } .timeline-scroll { padding: 12px; } }
</style>
