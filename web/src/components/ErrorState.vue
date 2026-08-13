<script setup lang="ts">
import { computed } from 'vue'
import { formatDeviceDateTime } from '../utils/deviceTime'

const props = defineProps<{
  title?: string
  message: string
  details?: string
  statusCode?: number
  requestMethod?: string
  requestUrl?: string
  lastSuccessAt?: number | null
  retryText?: string
}>()
const emit = defineEmits<{
  (e: 'retry'): void
}>()

const metaText = computed(() => {
  const parts: string[] = []
  if (props.statusCode) parts.push(`HTTP ${props.statusCode}`)
  const method = (props.requestMethod || '').toUpperCase()
  if (method && props.requestUrl) parts.push(`${method} ${props.requestUrl}`)
  else if (props.requestUrl) parts.push(String(props.requestUrl))
  if (props.lastSuccessAt) {
    parts.push(`最后成功：${formatDeviceDateTime(props.lastSuccessAt, { clientClock: true })}`)
  }
  return parts.join(' · ')
})
</script>

<template>
  <div class="error-state p-5 border">
    <div class="flex items-start justify-between gap-4">
      <div class="min-w-0">
        <div class="text-sm font-extrabold text-red-700 dark:text-red-300">{{ title || '加载失败' }}</div>
        <div class="mt-1 text-xs text-red-700/80 dark:text-red-200/80 break-words">{{ message }}</div>
        <div v-if="metaText" class="mt-2 text-xs text-red-800/60 dark:text-red-100/60 font-mono break-words">
          {{ metaText }}
        </div>
        <div v-if="details" class="mt-2 text-xs font-mono text-red-900/60 dark:text-red-100/60 whitespace-pre-wrap break-words">{{ details }}</div>
      </div>
      <el-button v-if="retryText" type="primary" @click="emit('retry')" class="!border-0">
        {{ retryText }}
      </el-button>
    </div>
  </div>
</template>

<style scoped>
.error-state {
  border-color: color-mix(in srgb, var(--ui-danger) 28%, var(--ui-border));
  border-radius: 6px;
  background: color-mix(in srgb, var(--ui-danger) 8%, var(--ui-surface));
  box-shadow: inset 3px 0 0 var(--ui-danger);
}
</style>
