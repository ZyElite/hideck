<script setup lang="ts">
import { computed } from 'vue'
import BalanceMessage from './BalanceMessage.vue'
import CommandAudioPlayer from './CommandAudioPlayer.vue'
import type { BalanceQuery, CommandEvent } from '../../types/commands'
import { formatDeviceDateTime } from '../../utils/deviceTime'
import { Bot24Regular } from '@vicons/fluent'

const props = defineProps<{
  events: CommandEvent[]
  balanceQueries: BalanceQuery[]
  loading: boolean
}>()

type TimelineItem =
  | { key: string; kind: 'command'; createdAt: string; event: CommandEvent }
  | { key: string; kind: 'balance'; createdAt: string; query: BalanceQuery }

const timelineItems = computed<TimelineItem[]>(() => {
  const commands: TimelineItem[] = props.events.map((event) => ({
    key: `command-${event.id}`, kind: 'command', createdAt: event.created_at, event
  }))
  const balances: TimelineItem[] = props.balanceQueries.map((query) => ({
    key: `balance-${query.id}`, kind: 'balance', createdAt: query.updated_at, query
  }))
  return [...commands, ...balances].sort((left, right) => Date.parse(left.createdAt) - Date.parse(right.createdAt))
})

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

function audioAttachments(event: CommandEvent) {
  return (event.attachments || []).filter((attachment) => attachment.type === 'audio')
}
</script>

<template>
  <section class="timeline" aria-label="命令对话记录">
    <div class="timeline-scroll" aria-live="polite">
      <div v-if="loading && !timelineItems.length" class="empty-line">正在读取命令记录</div>
      <div v-else-if="!timelineItems.length" class="empty-state">
        <el-icon><Bot24Regular /></el-icon>
        <strong>暂无命令记录</strong>
      </div>
      <article
        v-for="item in timelineItems"
        :key="item.key"
        class="message"
        :class="{
          user: item.kind === 'command' && item.event.kind === 'accepted',
          error: item.kind === 'command' && item.event.kind === 'error'
        }"
      >
        <div class="message-meta">
          <span>{{ item.kind === 'command' ? eventLabel(item.event) : 'VoHive' }}</span>
          <time>{{ formatDeviceDateTime(item.createdAt) }}</time>
        </div>
        <template v-if="item.kind === 'command'">
          <pre>{{ eventText(item.event) }}</pre>
          <CommandAudioPlayer
            v-for="attachment in audioAttachments(item.event)"
            :key="attachment.recording"
            :attachment="attachment"
          />
          <span v-if="item.event.execution?.state === 'running'" class="state running">执行中</span>
          <span v-else-if="item.event.execution?.state === 'failed'" class="state failed">失败</span>
        </template>
        <BalanceMessage v-else :query="item.query" />
      </article>
    </div>
  </section>
</template>

<style scoped>
.timeline { min-height: 0; display: flex; flex-direction: column; }
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
