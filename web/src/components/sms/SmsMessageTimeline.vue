<script setup lang="ts">
import { onUnmounted } from 'vue'
import { Delete24Regular } from '@vicons/fluent'
import type { SMSMessage } from '../../types/api'
import { formatDeviceDateTime } from '../../utils/deviceTime'
import EmptyState from '../EmptyState.vue'

defineProps<{
  groups: readonly Readonly<{ date: string; items: SMSMessage[] }>[]
  loading: boolean
  canLoadMore: boolean
  loadingMore: boolean
  deletingId: number | null
}>()

const emit = defineEmits<{
  loadMore: []
  delete: [message: SMSMessage]
  openActions: [message: SMSMessage]
}>()

const LONG_PRESS_MS = 450
const LONG_PRESS_MOVE_PX = 10
let longPressTimer: ReturnType<typeof setTimeout> | null = null
let startX = 0
let startY = 0

function clearLongPress() {
  if (longPressTimer == null) return
  clearTimeout(longPressTimer)
  longPressTimer = null
}

function beginLongPress(message: SMSMessage, event: PointerEvent) {
  if (event.pointerType === 'mouse') return
  clearLongPress()
  startX = event.clientX
  startY = event.clientY
  longPressTimer = setTimeout(() => {
    longPressTimer = null
    emit('openActions', message)
    navigator.vibrate?.(20)
  }, LONG_PRESS_MS)
}

function moveLongPress(event: PointerEvent) {
  if (longPressTimer == null) return
  if (Math.abs(event.clientX - startX) > LONG_PRESS_MOVE_PX || Math.abs(event.clientY - startY) > LONG_PRESS_MOVE_PX) {
    clearLongPress()
  }
}

function deliveryLabel(message: SMSMessage): string {
  if (message.type !== 2) return '已接收'
  if (message.status === 2) return '已发送'
  if (message.status === 3) return '发送失败'
  return '发送状态未确认'
}

function deliveryTone(message: SMSMessage): string {
  if (message.type !== 2) return 'is-received'
  if (message.status === 2) return 'is-success'
  if (message.status === 3) return 'is-danger'
  return 'is-muted'
}

onUnmounted(clearLongPress)
</script>

<template>
  <div class="sms-timeline">
    <div v-if="loading && groups.length === 0" class="sms-timeline-notice" role="status">正在加载短信历史…</div>
    <EmptyState
      v-else-if="groups.length === 0"
      class="sms-timeline-empty"
      title="暂无短信记录"
      subtitle="当前接口未返回此会话的历史短信"
    />

    <div v-if="canLoadMore" class="sms-load-more">
      <el-button text type="primary" :loading="loadingMore" @click="emit('loadMore')">加载更多</el-button>
    </div>

    <section v-for="group in groups" :key="group.date" class="sms-day-group">
      <time class="sms-day-label">{{ group.date }}</time>
      <article
        v-for="message in group.items"
        :key="message.id"
        class="sms-message"
        :class="message.type === 1 ? 'is-incoming' : 'is-outgoing'"
        @pointerdown="(event) => beginLongPress(message, event)"
        @pointermove="moveLongPress"
        @pointerup="clearLongPress"
        @pointercancel="clearLongPress"
      >
        <div class="sms-message-meta">
          <strong>{{ message.type === 1 ? (message.sender || '接收') : (message.device_name || '发送') }}</strong>
          <time>{{ formatDeviceDateTime(message.timestamp) }}</time>
          <span :class="deliveryTone(message)">{{ deliveryLabel(message) }}</span>
          <button
            type="button"
            class="sms-message-delete"
            :disabled="deletingId === message.id"
            :aria-label="`删除短信 ${message.id}`"
            title="删除短信"
            @click="emit('delete', message)"
          >
            <el-icon><Delete24Regular /></el-icon>
          </button>
        </div>
        <p>{{ message.content }}</p>
      </article>
    </section>
  </div>
</template>

<style scoped>
.sms-timeline { padding: 18px 22px 28px; }
.sms-timeline-notice,
.sms-timeline-empty { min-height: 240px; display: grid; place-items: center; color: var(--ui-text-muted); font-size: 12px; }
.sms-load-more { display: flex; justify-content: center; margin-bottom: 12px; }
.sms-day-group + .sms-day-group { margin-top: 22px; }
.sms-day-label { width: max-content; margin: 0 auto 18px; padding: 4px 10px; display: block; border: 1px solid var(--ui-border); border-radius: 999px; color: var(--ui-text-muted); font: 10px "v-mono", monospace; }

.sms-message { width: fit-content; max-width: 64%; margin-top: 16px; }
.sms-message.is-outgoing { margin-left: auto; }
.sms-message-meta { min-height: 22px; margin-bottom: 5px; display: flex; align-items: center; gap: 7px; color: var(--ui-text-muted); }
.sms-message.is-outgoing .sms-message-meta { justify-content: flex-end; }
.sms-message-meta strong { max-width: 150px; overflow: hidden; color: var(--ui-text); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.sms-message-meta time { font: 9px "v-mono", monospace; white-space: nowrap; }
.sms-message-meta span { font-size: 9px; white-space: nowrap; }
.sms-message-meta .is-success { color: var(--ui-success); }
.sms-message-meta .is-danger { color: var(--ui-danger); }

.sms-message p {
  min-width: 82px;
  margin: 0;
  padding: 10px 13px;
  border: 1px solid var(--ui-border);
  border-radius: 4px 12px 12px;
  background: var(--ui-surface-strong);
  color: var(--ui-text);
  font-size: 13px;
  line-height: 1.6;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.sms-message.is-outgoing p {
  border-color: color-mix(in srgb, var(--ui-primary) 38%, var(--ui-border));
  border-radius: 12px 4px 12px 12px;
  background: color-mix(in srgb, var(--ui-primary) 12%, var(--ui-surface));
}

.sms-message-delete { width: 26px; height: 26px; display: grid; place-items: center; border: 0; border-radius: 6px; background: transparent; color: var(--ui-danger); cursor: pointer; opacity: 0; pointer-events: none; transition: opacity 140ms var(--ui-ease-out); }
.sms-message:hover .sms-message-delete,
.sms-message:focus-within .sms-message-delete { opacity: 1; pointer-events: auto; }

@media (hover: none), (pointer: coarse) {
  .sms-message-delete { display: none; }
}

@media (max-width: 820px) {
  .sms-timeline { padding: 14px 12px 22px; }
  .sms-message { max-width: 82%; }
}

@media (max-width: 520px) {
  .sms-message { max-width: 94%; }
  .sms-message-meta { flex-wrap: wrap; gap: 5px; }
}

@media (prefers-reduced-motion: reduce) {
  .sms-message-delete { transition: none; }
}
</style>
