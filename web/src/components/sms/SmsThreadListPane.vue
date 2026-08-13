<script setup lang="ts">
import { computed, onUnmounted } from 'vue'
import { Add24Regular, Delete24Regular, Mail24Regular, Search24Regular } from '@vicons/fluent'
import { RecycleScroller } from 'vue-virtual-scroller'
import type { SmsThreadVM } from '../../types/view-model'
import { formatDeviceTime } from '../../utils/deviceTime'
import { smsPeerInitial, smsUnreadBadge } from '../../utils/smsPresentation'
import EmptyState from '../EmptyState.vue'
import ListSkeleton from '../ListSkeleton.vue'

type SmsThreadListItem = SmsThreadVM & Readonly<{ unreadCount?: number }>

const props = defineProps<{
  items: readonly SmsThreadListItem[]
  selectedKey: string
  modelValue: string
  loading: boolean
  deletingKey: string | null
}>()

const scrollerItems = computed(() => Array.from(props.items))

const emit = defineEmits<{
  'update:modelValue': [value: string]
  select: [key: string]
  delete: [thread: SmsThreadListItem]
  openActions: [thread: SmsThreadListItem]
  newMessage: []
}>()

const LONG_PRESS_MS = 450
const LONG_PRESS_MOVE_PX = 10
let longPressTimer: ReturnType<typeof setTimeout> | null = null
let longPressStartX = 0
let longPressStartY = 0

function clearLongPress() {
  if (longPressTimer == null) return
  clearTimeout(longPressTimer)
  longPressTimer = null
}

function startLongPress(thread: SmsThreadListItem, event: PointerEvent) {
  if (event.pointerType === 'mouse') return
  clearLongPress()
  longPressStartX = event.clientX
  longPressStartY = event.clientY
  longPressTimer = setTimeout(() => {
    longPressTimer = null
    emit('openActions', thread)
    if (typeof navigator !== 'undefined' && typeof navigator.vibrate === 'function') {
      navigator.vibrate(20)
    }
  }, LONG_PRESS_MS)
}

function moveLongPress(event: PointerEvent) {
  if (longPressTimer == null) return
  const movedX = Math.abs(event.clientX - longPressStartX)
  const movedY = Math.abs(event.clientY - longPressStartY)
  if (movedX > LONG_PRESS_MOVE_PX || movedY > LONG_PRESS_MOVE_PX) clearLongPress()
}

function updateSearch(value: unknown) {
  emit('update:modelValue', String(value || ''))
}

onUnmounted(clearLongPress)
</script>

<template>
  <section class="sms-thread-list" aria-label="短信会话">
    <header class="sms-thread-toolbar">
      <el-input
        :model-value="modelValue"
        placeholder="搜索联系人/内容"
        clearable
        aria-label="搜索短信会话"
        @update:model-value="updateSearch"
      >
        <template #prefix><el-icon><Search24Regular /></el-icon></template>
      </el-input>
      <el-button class="sms-new-message" type="primary" aria-label="新建短信" @click="emit('newMessage')">
        <el-icon><Add24Regular /></el-icon>
        <span>新建</span>
      </el-button>
    </header>

    <ListSkeleton v-if="loading && items.length === 0" :rows="8" />

    <div v-else-if="items.length === 0" class="sms-thread-empty">
      <EmptyState title="暂无会话" subtitle="等待设备收到短信或新建短信">
        <template #icon><el-icon size="26"><Mail24Regular /></el-icon></template>
      </EmptyState>
    </div>

    <RecycleScroller
      v-else
      :items="scrollerItems"
      :item-size="82"
      key-field="key"
      class="sms-thread-scroll"
    >
      <template #default="{ item: thread }">
        <article
          class="sms-thread-row"
          :class="{ 'is-active': selectedKey === thread.key }"
          @pointerdown="(event) => startLongPress(thread, event)"
          @pointermove="moveLongPress"
          @pointerup="clearLongPress"
          @pointercancel="clearLongPress"
        >
          <button
            type="button"
            class="sms-thread-select"
            :aria-current="selectedKey === thread.key ? 'true' : undefined"
            @click="emit('select', thread.key)"
          >
            <span class="sms-thread-avatar" aria-hidden="true">{{ smsPeerInitial(thread.peer) }}</span>
            <span class="sms-thread-copy">
              <strong>{{ thread.peer || '未知联系人' }}</strong>
              <small>{{ thread.lastMessage || '暂无短信内容' }}</small>
            </span>
            <span class="sms-thread-meta">
              <time>{{ formatDeviceTime(thread.lastTs, { fallback: '时间未知' }) }}</time>
              <small>{{ thread.localPhone || thread.lastDeviceName || '设备未提供' }}</small>
              <i v-if="smsUnreadBadge(thread.unreadCount || 0)" aria-label="未读消息">
                {{ Math.min(smsUnreadBadge(thread.unreadCount || 0), 99) }}
              </i>
            </span>
          </button>
          <button
            type="button"
            class="sms-thread-delete"
            :class="{ 'is-loading': deletingKey === thread.key }"
            :disabled="deletingKey === thread.key"
            :aria-label="`删除与 ${thread.peer} 的对话`"
            title="删除对话"
            @click.stop="emit('delete', thread)"
          >
            <el-icon><Delete24Regular /></el-icon>
          </button>
        </article>
      </template>
    </RecycleScroller>
  </section>
</template>

<style scoped>
.sms-thread-list {
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--ui-border);
  background: var(--ui-surface);
}

.sms-thread-toolbar {
  min-height: 64px;
  padding: 10px 12px;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
  border-bottom: 1px solid var(--ui-border);
}

.sms-new-message { min-width: 74px; }
.sms-thread-scroll { min-height: 0; flex: 1; overflow: auto; }
.sms-thread-empty { min-height: 190px; flex: 1; display: grid; place-items: center; padding: 18px; }

.sms-thread-row {
  position: relative;
  height: 82px;
  border-bottom: 1px solid var(--ui-border-muted);
  background: transparent;
  transition: background-color 150ms var(--ui-ease-out), box-shadow 150ms var(--ui-ease-out);
}

.sms-thread-row.is-active {
  background: color-mix(in srgb, var(--ui-primary) 11%, var(--ui-surface));
  box-shadow: 2px 0 0 var(--ui-primary) inset;
}

.sms-thread-select {
  width: 100%;
  height: 100%;
  padding: 11px 12px;
  display: grid;
  grid-template-columns: 38px minmax(0, 1fr) 66px;
  align-items: center;
  gap: 10px;
  border: 0;
  background: transparent;
  color: var(--ui-text);
  cursor: pointer;
  text-align: left;
}

.sms-thread-avatar {
  width: 38px;
  height: 38px;
  display: grid;
  place-items: center;
  border: 1px solid var(--ui-border);
  border-radius: 50%;
  background: var(--ui-surface-strong);
  color: var(--ui-text);
  font-size: 13px;
}

.sms-thread-copy,
.sms-thread-meta { min-width: 0; }

.sms-thread-copy strong,
.sms-thread-copy small,
.sms-thread-meta small {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sms-thread-copy strong { font-size: 13px; }
.sms-thread-copy small { margin-top: 5px; color: var(--ui-text-muted); font-size: var(--ui-font-body-sm); }

.sms-thread-meta {
  align-self: stretch;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  justify-content: center;
  color: var(--ui-text-muted);
  text-align: right;
}

.sms-thread-meta time { font: var(--ui-font-caption) "v-mono", monospace; }
.sms-thread-meta small { max-width: 66px; margin-top: 4px; font-size: var(--ui-font-caption); }

.sms-thread-meta i {
  position: absolute;
  right: 10px;
  bottom: 9px;
  min-width: 20px;
  height: 18px;
  padding: 0 5px;
  display: grid;
  place-items: center;
  border-radius: 6px;
  background: var(--ui-primary);
  color: var(--ui-surface-subtle);
  font-size: var(--ui-font-caption);
  font-style: normal;
  font-weight: 700;
}

.sms-thread-delete {
  position: absolute;
  z-index: 2;
  top: 50%;
  right: 10px;
  width: 34px;
  height: 34px;
  display: grid;
  place-items: center;
  border: 1px solid color-mix(in srgb, var(--ui-danger) 30%, var(--ui-border));
  border-radius: 6px;
  background: color-mix(in srgb, var(--ui-danger) 8%, var(--ui-surface));
  color: var(--ui-danger);
  cursor: pointer;
  opacity: 0;
  pointer-events: none;
  transform: translate(7px, -50%);
  transition: opacity 140ms var(--ui-ease-out), transform 140ms var(--ui-ease-out);
}

.sms-thread-delete.is-loading { cursor: wait; opacity: .55; }

@media (hover: hover) and (pointer: fine) {
  .sms-thread-row:hover:not(.is-active) { background: var(--ui-surface-muted); }
  .sms-thread-row:hover .sms-thread-delete,
  .sms-thread-row:focus-within .sms-thread-delete { opacity: 1; pointer-events: auto; transform: translate(0, -50%); }
  .sms-thread-row:hover .sms-thread-meta,
  .sms-thread-row:focus-within .sms-thread-meta { opacity: 0; }
}

@media (max-width: 820px) {
  .sms-thread-toolbar { min-height: 64px; padding: 10px; }
  .sms-new-message { width: 44px; min-width: 44px; height: 44px; padding: 0; }
  .sms-new-message span { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
  .sms-thread-row { height: 82px; }
  .sms-thread-select { min-height: 82px; padding-inline: 10px; grid-template-columns: 38px minmax(0, 1fr) 58px; }
  .sms-thread-delete { display: none; }
}

@media (prefers-reduced-motion: reduce) {
  .sms-thread-row { transition: background-color 120ms linear; }
  .sms-thread-delete { transition: opacity 120ms linear; transform: translateY(-50%); }
}
</style>
