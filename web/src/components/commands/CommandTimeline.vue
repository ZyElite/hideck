<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import BalanceMessage from './BalanceMessage.vue'
import CommandAudioPlayer from './CommandAudioPlayer.vue'
import type { BalanceQuery, CommandEvent } from '../../types/commands'
import { formatDeviceDateTime } from '../../utils/deviceTime'
import { countAddedTimelineRecords, isNearTimelineEnd } from '../../utils/timelineFollow'
import {
  presentBalanceState,
  presentCommandEvent,
  type BalanceStatePresentation,
  type CommandEventPresentation
} from '../../utils/commandPresentation'
import {
  Bot24Regular,
  CheckmarkCircle24Regular,
  Clock24Regular,
  ErrorCircle24Regular,
  Send24Regular,
  Wallet24Regular
} from '@vicons/fluent'

const props = defineProps<{
  events: CommandEvent[]
  balanceQueries: BalanceQuery[]
  loading: boolean
  contextKey: string
  historyVersion: number
}>()

type TimelineAnchor = Readonly<{ key: string; offset: number }>

const timelineScroll = ref<HTMLElement | null>(null)
const followingLatest = ref(true)
const pendingRecordCount = ref(0)
let previousScrollTop = 0

type TimelineItem =
  | {
    key: string
    kind: 'command'
    createdAt: string
    event: CommandEvent
    presentation: CommandEventPresentation
  }
  | {
    key: string
    kind: 'balance'
    createdAt: string
    query: BalanceQuery
    presentation: BalanceStatePresentation
  }

const timelineItems = computed<TimelineItem[]>(() => {
  const commands: TimelineItem[] = props.events.map((event) => ({
    key: `command-${event.id}`,
    kind: 'command',
    createdAt: event.created_at,
    event,
    presentation: presentCommandEvent(event)
  }))
  const balances: TimelineItem[] = props.balanceQueries.map((query) => ({
    key: `balance-${query.id}`,
    kind: 'balance',
    createdAt: query.updated_at,
    query,
    presentation: presentBalanceState(query)
  }))
  return [...commands, ...balances].sort((left, right) => (
    Date.parse(left.createdAt) - Date.parse(right.createdAt)
  ))
})

const timelineKeys = computed(() => timelineItems.value.map((item) => item.key))

onMounted(async () => {
  await nextTick()
  scrollToLatest('auto')
})

watch(
  [timelineKeys, () => props.contextKey, () => props.historyVersion],
  async ([nextKeys, nextContext, nextHistory], [previousKeys, previousContext, previousHistory]) => {
    const container = timelineScroll.value
    if (!container) return

    const contextChanged = nextContext !== previousContext
    const historyPrepended = nextHistory !== previousHistory
    const anchor = historyPrepended ? captureVisibleAnchor(container) : null
    const addedCount = countAddedTimelineRecords(previousKeys, nextKeys)

    await nextTick()
    if (contextChanged) {
      scrollToLatest('auto')
      return
    }
    if (historyPrepended) {
      restoreVisibleAnchor(container, anchor)
      return
    }
    if (!previousKeys.length && nextKeys.length) {
      scrollToLatest('auto')
      return
    }
    if (!addedCount) return
    if (followingLatest.value) {
      scrollToLatest(preferredScrollBehavior())
      return
    }
    pendingRecordCount.value += addedCount
  },
  { flush: 'pre' }
)

function captureVisibleAnchor(container: HTMLElement): TimelineAnchor | null {
  const containerTop = container.getBoundingClientRect().top
  const elements = container.querySelectorAll<HTMLElement>('[data-timeline-key]')
  for (const element of elements) {
    const bounds = element.getBoundingClientRect()
    if (bounds.bottom <= containerTop) continue
    return { key: element.dataset.timelineKey || '', offset: bounds.top - containerTop }
  }
  return null
}

function restoreVisibleAnchor(container: HTMLElement, anchor: TimelineAnchor | null) {
  if (!anchor?.key) return
  const element = [...container.querySelectorAll<HTMLElement>('[data-timeline-key]')]
    .find((candidate) => candidate.dataset.timelineKey === anchor.key)
  if (!element) return
  const nextOffset = element.getBoundingClientRect().top - container.getBoundingClientRect().top
  container.scrollTop += nextOffset - anchor.offset
  previousScrollTop = container.scrollTop
}

function preferredScrollBehavior(): ScrollBehavior {
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth'
}

function scrollToLatest(behavior: ScrollBehavior) {
  const container = timelineScroll.value
  if (!container) return
  followingLatest.value = true
  pendingRecordCount.value = 0
  container.scrollTo({ top: container.scrollHeight, behavior })
  previousScrollTop = container.scrollTop
}

function handleTimelineScroll() {
  const container = timelineScroll.value
  if (!container) return
  if (container.scrollTop < previousScrollTop) followingLatest.value = false
  if (isNearTimelineEnd(container)) {
    followingLatest.value = true
    pendingRecordCount.value = 0
  }
  previousScrollTop = container.scrollTop
}

function audioAttachments(event: CommandEvent) {
  return (event.attachments || []).filter((attachment) => attachment.type === 'audio')
}
</script>

<template>
  <section class="timeline" aria-label="命令执行记录">
    <div ref="timelineScroll" class="timeline-scroll" aria-live="polite" @scroll.passive="handleTimelineScroll">
      <div v-if="loading && !timelineItems.length" class="empty-line">
        <span class="empty-icon" aria-hidden="true"><el-icon><Clock24Regular /></el-icon></span>
        <strong>正在读取命令记录</strong>
      </div>
      <div v-else-if="!timelineItems.length" class="empty-state">
        <span class="empty-icon" aria-hidden="true"><el-icon><Bot24Regular /></el-icon></span>
        <strong>暂无命令记录</strong>
        <span>选择下方真实命令后，执行过程会显示在这里</span>
      </div>
      <div v-else class="timeline-track">
        <article
          v-for="item in timelineItems"
          :key="item.key"
          :data-timeline-key="item.key"
          class="timeline-event"
          :class="`tone-${item.presentation.tone}`"
        >
          <span class="event-marker" aria-hidden="true">
            <el-icon v-if="item.kind === 'balance'"><Wallet24Regular /></el-icon>
            <el-icon v-else-if="item.presentation.tone === 'sent'"><Send24Regular /></el-icon>
            <el-icon v-else-if="item.presentation.tone === 'running'"><Clock24Regular /></el-icon>
            <el-icon v-else-if="item.presentation.tone === 'danger'"><ErrorCircle24Regular /></el-icon>
            <el-icon v-else><CheckmarkCircle24Regular /></el-icon>
          </span>

          <div class="event-card">
            <header>
              <strong>{{ item.kind === 'balance' ? '运营商余额' : item.presentation.title }}</strong>
              <time>{{ formatDeviceDateTime(item.createdAt) }}</time>
            </header>
            <template v-if="item.kind === 'command'">
              <pre>{{ item.presentation.detail }}</pre>
              <CommandAudioPlayer
                v-for="attachment in audioAttachments(item.event)"
                :key="attachment.recording"
                :attachment="attachment"
              />
              <span v-if="item.event.execution?.command" class="event-command">
                /{{ item.event.execution.command }}
              </span>
            </template>
            <BalanceMessage v-else :query="item.query" />
          </div>
        </article>
      </div>
    </div>
    <el-button
      v-if="pendingRecordCount"
      class="new-records"
      type="primary"
      round
      @click="scrollToLatest(preferredScrollBehavior())"
    >
      {{ pendingRecordCount }} 条新记录 · 查看最新
    </el-button>
  </section>
</template>

<style scoped>
.timeline { position: relative; min-height: 0; display: flex; flex-direction: column; }
.timeline-scroll {
  position: relative;
  min-height: 0;
  overflow: auto;
  padding: 16px 20px 18px;
  overscroll-behavior: contain;
  scrollbar-width: thin;
}
.timeline-track { position: relative; display: grid; gap: 8px; padding-left: 54px; }
.timeline-track::before {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: 20px;
  width: 1px;
  background: color-mix(in srgb, var(--ui-primary) 26%, var(--ui-border));
}
.timeline-event { position: relative; min-width: 0; color: var(--ui-primary); animation: event-enter 180ms ease-out both; }
.event-marker {
  position: absolute;
  top: 8px;
  left: -54px;
  z-index: 1;
  width: 40px;
  height: 40px;
  border: 1px solid currentColor;
  border-radius: 50%;
  background: var(--ui-surface);
  display: grid;
  place-items: center;
  box-shadow: 0 0 0 5px var(--ui-surface), 0 0 18px color-mix(in srgb, currentColor 10%, transparent);
}
.event-marker .el-icon { font-size: 19px; }
.event-card {
  min-width: 0;
  min-height: 58px;
  padding: 10px 14px;
  border: 1px solid var(--ui-border);
  border-radius: 7px;
  background: color-mix(in srgb, var(--ui-surface-strong) 76%, transparent);
}
.event-card header { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; }
.event-card header strong { color: currentColor; font-size: 13px; font-weight: 700; }
.event-card time { flex: 0 0 auto; color: var(--ui-text-subtle); font: 10px "v-mono", ui-monospace, monospace; }
.event-card pre {
  margin: 3px 0 0;
  color: var(--ui-text-muted);
  font: 12px/1.5 "v-mono", ui-monospace, monospace;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.event-command { display: inline-block; margin-top: 7px; color: var(--ui-text-subtle); font: 10px "v-mono", monospace; }
.tone-running, .tone-waiting { color: var(--ui-warning); }
.tone-parsed { color: var(--ui-info); }
.tone-danger { color: var(--ui-danger); }
.empty-state, .empty-line {
  min-height: 300px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 7px;
  color: var(--ui-text-subtle);
  text-align: center;
}
.empty-icon {
  width: 42px;
  height: 42px;
  margin-bottom: 3px;
  border: 1px solid var(--ui-border);
  border-radius: 6px;
  display: grid;
  place-items: center;
  color: var(--ui-primary);
}
.empty-icon .el-icon { font-size: 22px; }
.empty-state strong, .empty-line strong { color: var(--ui-text); font-size: 13px; }
.empty-state > span:last-child { font-size: 11px; }
.new-records {
  position: absolute;
  z-index: 10;
  left: 50%;
  bottom: 12px;
  min-height: 36px;
  transform: translateX(-50%);
  box-shadow: 0 8px 24px color-mix(in srgb, var(--ui-bg) 52%, transparent);
}
@keyframes event-enter {
  from { opacity: 0; transform: translateY(4px); }
  to { opacity: 1; transform: translateY(0); }
}
@media (max-width: 640px) {
  .timeline-scroll { padding: 14px 10px 18px; }
  .timeline-track { padding-left: 48px; }
  .timeline-track::before { left: 18px; }
  .event-marker { left: -48px; width: 36px; height: 36px; }
  .event-card { padding: 10px 12px; }
  .event-card header { align-items: flex-start; flex-direction: column; gap: 2px; }
  .new-records { min-height: 44px; }
}
@media (prefers-reduced-motion: reduce) {
  .timeline-event { animation-name: event-fade; animation-duration: 120ms; }
  @keyframes event-fade { from { opacity: .72; } to { opacity: 1; } }
}
</style>
