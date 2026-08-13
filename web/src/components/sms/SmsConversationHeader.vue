<script setup lang="ts">
import {
  ArrowDown24Regular,
  ArrowLeft24Regular,
  ArrowSync24Regular,
  Delete24Regular
} from '@vicons/fluent'
import type { SmsConversationContext } from '../../utils/smsPresentation'

defineProps<{
  peer: string
  context: SmsConversationContext
  loading: boolean
  showBack: boolean
}>()

const emit = defineEmits<{
  back: []
  refresh: []
  latest: []
  delete: []
}>()
</script>

<template>
  <header class="sms-conversation-header">
    <div class="sms-conversation-identity">
      <button v-if="showBack" type="button" class="sms-header-icon sms-back" aria-label="返回会话列表" @click="emit('back')">
        <el-icon><ArrowLeft24Regular /></el-icon>
      </button>
      <div class="sms-conversation-copy">
        <strong>{{ peer || '请选择会话' }}</strong>
        <span>{{ context.deviceLabel }} · {{ context.operatorLabel }}</span>
      </div>
    </div>

    <div v-if="peer" class="sms-context-and-actions">
      <div class="sms-runtime-facts" aria-label="短信运行状态">
        <span :class="`is-${context.smsTone}`">{{ context.smsLabel }}</span>
        <span :class="`is-${context.imsTone}`">{{ context.imsLabel }}</span>
      </div>
      <nav aria-label="会话操作">
        <button type="button" class="sms-header-icon" :disabled="loading" aria-label="刷新会话" title="刷新" @click="emit('refresh')">
          <el-icon :class="{ 'is-loading': loading }"><ArrowSync24Regular /></el-icon>
        </button>
        <button type="button" class="sms-header-icon" aria-label="跳到最新短信" title="最新" @click="emit('latest')">
          <el-icon><ArrowDown24Regular /></el-icon>
        </button>
        <button type="button" class="sms-header-icon is-danger" aria-label="删除当前对话" title="删除对话" @click="emit('delete')">
          <el-icon><Delete24Regular /></el-icon>
        </button>
      </nav>
    </div>
  </header>
</template>

<style scoped>
.sms-conversation-header {
  min-width: 0;
  min-height: 64px;
  padding: 9px 12px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  border-bottom: 1px solid var(--ui-border);
  background: var(--ui-surface);
}

.sms-conversation-identity,
.sms-context-and-actions,
.sms-runtime-facts,
.sms-context-and-actions nav { display: flex; align-items: center; }

.sms-conversation-identity { min-width: 0; gap: 8px; }
.sms-context-and-actions { min-width: 0; gap: 14px; }
.sms-runtime-facts { gap: 6px; }
.sms-context-and-actions nav { flex: none; gap: 4px; }
.sms-conversation-copy { min-width: 0; }

.sms-conversation-copy strong,
.sms-conversation-copy span { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sms-conversation-copy strong { color: var(--ui-text); font-size: 14px; }
.sms-conversation-copy span { margin-top: 3px; color: var(--ui-text-muted); font-size: var(--ui-font-body-sm); }

.sms-runtime-facts span {
  padding: 4px 7px;
  border: 1px solid var(--ui-border);
  border-radius: 999px;
  color: var(--ui-text-muted);
  font-size: var(--ui-font-caption);
  white-space: nowrap;
}

.sms-runtime-facts .is-success { border-color: color-mix(in srgb, var(--ui-success) 35%, var(--ui-border)); color: var(--ui-success); }
.sms-runtime-facts .is-danger { border-color: color-mix(in srgb, var(--ui-danger) 35%, var(--ui-border)); color: var(--ui-danger); }

.sms-header-icon {
  width: 34px;
  height: 34px;
  display: grid;
  place-items: center;
  border: 1px solid transparent;
  border-radius: 7px;
  background: transparent;
  color: var(--ui-text-muted);
  cursor: pointer;
}

.sms-header-icon:hover { border-color: var(--ui-border); background: var(--ui-surface-muted); color: var(--ui-text); }
.sms-header-icon:disabled { cursor: wait; opacity: .55; }
.sms-header-icon.is-danger:hover { color: var(--ui-danger); }
.sms-back { display: none; }

@media (max-width: 1120px) {
  .sms-runtime-facts { display: none; }
}

@media (max-width: 979px) {
  .sms-back { display: grid; }
}

@media (max-width: 520px) {
  .sms-conversation-header { padding-inline: 8px; }
  .sms-header-icon { width: 44px; height: 44px; }
  .sms-context-and-actions nav { gap: 0; }
  .sms-conversation-copy span { max-width: 130px; }
}
</style>
