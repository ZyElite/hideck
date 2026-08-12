<script setup lang="ts">
defineProps<{
  title: string
  subtitle?: string
}>()
</script>

<template>
  <header class="page-header">
    <div class="page-heading">
      <div>
        <span class="page-heading-kicker">VOHIVE / CONTROL PLANE</span>
        <h1>{{ title }}</h1>
        <p v-if="subtitle">{{ subtitle }}</p>
      </div>
    </div>
    <div class="page-actions">
      <slot name="actions" />
    </div>
  </header>
</template>

<style scoped>
.page-header {
  min-height: 54px;
  margin-bottom: 20px;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
}

.page-heading {
  min-width: 0;
  display: flex;
  align-items: flex-start;
  gap: 0;
}

h1 {
  margin: 5px 0 0;
  color: var(--ui-text);
  font-size: 28px;
  font-weight: 590;
  line-height: 1.15;
  letter-spacing: 0;
}

.page-heading-kicker {
  color: var(--ui-primary);
  font-family: "v-mono", ui-monospace, monospace;
  font-size: 9px;
  font-weight: 700;
  letter-spacing: 0.14em;
}

p {
  margin: 5px 0 0;
  color: var(--ui-text-muted);
  font-size: 13px;
  line-height: 1.4;
}

.page-actions {
  flex: 0 0 auto;
}

@media (max-width: 640px) {
  .page-header {
    min-height: auto;
    margin-bottom: 16px;
    flex-direction: column;
    gap: 12px;
  }

  h1 {
    font-size: 20px;
  }

  .page-actions {
    width: 100%;
  }

  .page-actions :deep(> *) {
    max-width: 100%;
    flex-wrap: wrap;
  }
}
</style>
