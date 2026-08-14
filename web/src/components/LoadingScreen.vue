<script setup lang="ts">
defineProps({
  title: { type: String, default: '正在加载…' },
  subtitle: { type: String, default: '正在读取设备与服务状态' }
})
</script>

<template>
  <div class="loading-screen" role="status" aria-live="polite">
    <div class="loading-state">
      <div class="loading-context">
        <span class="loading-dot" aria-hidden="true" />
        <span>HIDECK CONTROL PLANE</span>
      </div>
      <div class="loading-copy">
        <strong>{{ title }}</strong>
        <span>{{ subtitle }}</span>
      </div>
      <span class="loading-line" aria-hidden="true" />
    </div>
  </div>
</template>

<style scoped>
.loading-screen {
  position: relative;
  width: 100%;
  min-height: 260px;
  display: grid;
  place-items: center;
  overflow: hidden;
  background: radial-gradient(circle at 50% 50%, color-mix(in srgb, var(--ui-primary) 7%, transparent), transparent 34%);
}

.loading-state {
  width: min(calc(100% - 40px), 360px);
  display: grid;
  gap: 10px;
  animation: loading-state-enter 220ms var(--ui-ease-out) both;
}

.loading-context {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--ui-primary);
  font: 700 var(--ui-font-caption) "v-mono", ui-monospace, monospace;
  letter-spacing: .14em;
}

.loading-dot {
  width: 6px;
  height: 6px;
  flex: 0 0 6px;
  border-radius: 50%;
  background: var(--ui-primary);
  box-shadow: 0 0 12px color-mix(in srgb, var(--ui-primary) 55%, transparent);
}

.loading-copy {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.loading-copy strong {
  color: var(--ui-text);
  font-size: 18px;
  font-weight: 650;
  line-height: 1.35;
}

.loading-copy span {
  color: var(--ui-text-muted);
  font-size: var(--ui-font-body-sm);
  line-height: 1.5;
}

.loading-line {
  width: 100%;
  height: 1px;
  margin-top: 4px;
  background: linear-gradient(90deg, var(--ui-primary), color-mix(in srgb, var(--ui-primary) 18%, transparent) 46%, transparent 82%);
}

@keyframes loading-state-enter {
  from { opacity: 0; transform: translateY(6px); }
  to { opacity: 1; transform: translateY(0); }
}

@media (prefers-reduced-motion: reduce) {
  .loading-state { animation-name: loading-state-fade; }

  @keyframes loading-state-fade {
    from { opacity: 0; }
    to { opacity: 1; }
  }
}
</style>
