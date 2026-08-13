<script setup lang="ts">
defineProps<{
  compact?: boolean
  title: string
  subtitle?: string
}>()
</script>

<template>
  <div
    class="empty-state text-center ui-surface-muted"
    :class="compact ? 'is-compact' : 'p-10 border border-dashed'"
  >
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

.empty-state.is-compact {
  min-height: 156px;
  padding: 24px 16px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  border: 0;
  border-radius: 0;
  background: var(--ui-surface);
}

.empty-state.is-compact .empty-state-icon {
  width: 36px;
  height: 36px;
}

.empty-state.is-compact > .mt-4 {
  margin-top: 10px;
}

.empty-state.is-compact > .mt-5 {
  margin-top: 0;
}
</style>
