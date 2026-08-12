<script setup lang="ts">
defineProps<{
  title: string
  subtitle?: string
}>()
</script>

<template>
  <div class="empty-state p-10 text-center border border-dashed ui-surface-muted">
    <div class="empty-state-icon mx-auto w-11 h-11 flex items-center justify-center">
      <slot name="icon">
        <span class="text-xl font-bold">∅</span>
      </slot>
    </div>
    <div class="mt-4 text-sm font-bold text-gray-700 dark:text-gray-200">{{ title }}</div>
    <div v-if="subtitle" class="mt-1 text-xs text-gray-400">{{ subtitle }}</div>
    <div class="mt-5 flex items-center justify-center gap-2">
      <slot name="actions" />
    </div>
  </div>
</template>

<style scoped>
.empty-state {
  border-color: var(--ui-border);
  border-radius: 6px;
}

.empty-state-icon {
  border: 1px solid var(--ui-border);
  border-radius: 5px;
  background: var(--ui-surface);
  color: var(--ui-text-muted);
}
</style>
