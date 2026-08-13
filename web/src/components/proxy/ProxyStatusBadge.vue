<script setup lang="ts">
import type { ProxyPresentationTone } from '../../utils/proxyPresentation'

defineProps<{
  label: string
  tone: ProxyPresentationTone
}>()
</script>

<template>
  <span class="proxy-status-badge" :class="`is-${tone}`">
    <i aria-hidden="true" />
    {{ label }}
  </span>
</template>

<style scoped>
.proxy-status-badge {
  width: fit-content;
  max-width: 100%;
  min-height: 26px;
  padding: 4px 8px;
  display: inline-flex;
  align-items: center;
  gap: 7px;
  border: 1px solid var(--ui-border-muted);
  border-radius: 999px;
  background: var(--ui-surface);
  color: var(--ui-text-muted);
  font-size: 11px;
  line-height: 1.2;
}

.proxy-status-badge i {
  width: 6px;
  height: 6px;
  flex: 0 0 6px;
  border-radius: 50%;
  background: currentColor;
}

.proxy-status-badge.is-success {
  border-color: color-mix(in srgb, var(--ui-success) 28%, var(--ui-border));
  background: color-mix(in srgb, var(--ui-success) 7%, var(--ui-surface));
  color: var(--ui-success);
}

.proxy-status-badge.is-warning {
  border-color: color-mix(in srgb, var(--ui-warning) 30%, var(--ui-border));
  background: color-mix(in srgb, var(--ui-warning) 7%, var(--ui-surface));
  color: var(--ui-warning);
}

.proxy-status-badge.is-danger {
  border-color: color-mix(in srgb, var(--ui-danger) 30%, var(--ui-border));
  background: color-mix(in srgb, var(--ui-danger) 7%, var(--ui-surface));
  color: var(--ui-danger);
}
</style>
