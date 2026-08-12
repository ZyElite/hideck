<script setup lang="ts">
defineProps({
  title: { type: String, default: '正在加载…' },
  subtitle: { type: String, default: '正在读取设备与服务状态' }
})
</script>

<template>
  <div class="loading-screen min-h-[260px] w-full flex items-center justify-center" role="status" aria-live="polite">
    <div class="loading-panel">
      <span class="loading-mark">V</span>
      <div class="loading-copy">
        <strong>{{ title }}</strong>
        <span>{{ subtitle }}</span>
      </div>
      <span class="loading-spinner" aria-hidden="true" />
    </div>
  </div>
</template>

<style scoped>
.loading-panel {
  width: min(100%, 420px);
  min-height: 72px;
  padding: 14px 16px;
  display: grid;
  grid-template-columns: 38px minmax(0, 1fr) 20px;
  align-items: center;
  gap: 13px;
  border: 1px solid var(--ui-border);
  border-radius: 6px;
  background: var(--ui-surface);
  box-shadow: var(--ui-shadow-sm);
}

.loading-mark {
  width: 38px;
  height: 38px;
  display: grid;
  place-items: center;
  border-radius: 5px;
  background: var(--ui-primary-solid);
  color: #fff;
  font-weight: 700;
}

.loading-copy {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.loading-copy strong {
  overflow: hidden;
  color: var(--ui-text);
  font-size: 14px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.loading-copy span {
  overflow: hidden;
  color: var(--ui-text-muted);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.loading-spinner {
  width: 18px;
  height: 18px;
  border: 2px solid color-mix(in srgb, var(--ui-primary) 25%, var(--ui-border));
  border-top-color: var(--ui-primary);
  border-radius: 50%;
  animation: spin .72s linear infinite;
}

@keyframes spin { to { transform: rotate(360deg); } }
</style>
