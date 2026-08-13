<script setup lang="ts">
import { Send24Regular } from '@vicons/fluent'

defineProps<{
  modelValue: string
  encoding: string
  parts: number
  length: number
  sending: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
  send: []
}>()

function updateMessage(value: unknown) {
  emit('update:modelValue', String(value || ''))
}
</script>

<template>
  <footer class="sms-composer">
    <el-input
      :model-value="modelValue"
      type="textarea"
      :autosize="{ minRows: 1, maxRows: 6 }"
      resize="none"
      placeholder="输入回复"
      aria-label="短信回复内容"
      @update:model-value="updateMessage"
      @keydown.enter.exact.prevent="emit('send')"
    />
    <button type="button" class="sms-send" :disabled="sending || !modelValue.trim()" @click="emit('send')">
      <el-icon><Send24Regular /></el-icon>
      <span>{{ sending ? '发送中' : '发送' }}</span>
    </button>
    <div class="sms-composer-meta">
      <span>{{ encoding }} · 预计 {{ parts }} 段 · {{ length }} 字</span>
      <span>Enter 发送 · Shift+Enter 换行</span>
    </div>
  </footer>
</template>

<style scoped>
.sms-composer {
  padding: 12px 14px 10px;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px 10px;
  border-top: 1px solid var(--ui-border);
  background: var(--ui-surface);
}

.sms-send {
  min-width: 82px;
  min-height: 40px;
  padding: 0 14px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  align-self: end;
  border: 1px solid var(--ui-primary);
  border-radius: 7px;
  background: var(--ui-primary);
  color: var(--ui-surface-subtle);
  cursor: pointer;
  font-weight: 700;
}

.sms-send:disabled { cursor: not-allowed; opacity: .5; }
.sms-composer-meta { grid-column: 1 / -1; display: flex; justify-content: space-between; gap: 12px; color: var(--ui-text-muted); font-size: 9px; }

@media (max-width: 520px) {
  .sms-composer {
    padding: 10px 9px;
    grid-template-columns: minmax(0, 1fr) 48px;
    scroll-margin-bottom: calc(76px + env(safe-area-inset-bottom));
  }
  .sms-send { min-width: 48px; width: 48px; min-height: 44px; padding: 0; }
  .sms-send span { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
  .sms-composer-meta span:last-child { display: none; }
}
</style>
