<script setup lang="ts">
import { Add24Regular, ArrowClockwise24Regular } from '@vicons/fluent'
import EmptyState from '../EmptyState.vue'
import ListSkeleton from '../ListSkeleton.vue'

defineProps<{
  addLabel: string
  empty: boolean
  emptySubtitle: string
  emptyTitle: string
  kicker: string
  loading: boolean
  refreshing: boolean
  subtitle: string
  title: string
  titleId: string
  tone: 'communication' | 'primary'
}>()

defineEmits<{
  add: []
  refresh: []
}>()
</script>

<template>
  <section class="proxy-inventory ui-card" :aria-labelledby="titleId">
    <header class="proxy-inventory-header">
      <div class="proxy-inventory-heading">
        <span class="section-icon" :class="`section-icon-${tone}`" aria-hidden="true">
          <slot name="icon" />
        </span>
        <div>
          <span>{{ kicker }}</span>
          <h2 :id="titleId">{{ title }}</h2>
          <p>{{ subtitle }}</p>
        </div>
      </div>

      <div class="proxy-inventory-actions">
        <el-button :loading="refreshing" @click="$emit('refresh')">
          <el-icon aria-hidden="true"><ArrowClockwise24Regular /></el-icon>
          {{ refreshing ? '刷新中' : '刷新' }}
        </el-button>
        <el-button type="primary" @click="$emit('add')">
          <el-icon aria-hidden="true"><Add24Regular /></el-icon>
          {{ addLabel }}
        </el-button>
      </div>
    </header>

    <ListSkeleton v-if="loading && empty" :rows="3" />
    <EmptyState
      v-else-if="empty"
      compact
      :title="emptyTitle"
      :subtitle="emptySubtitle"
    />
    <slot v-else />
  </section>
</template>

<style scoped>
.proxy-inventory {
  padding: 0;
  overflow: hidden;
}

.proxy-inventory-header {
  min-height: 76px;
  padding: 12px 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  border-bottom: 1px solid var(--ui-border);
}

.proxy-inventory-heading {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 12px;
}

.proxy-inventory-heading > div {
  min-width: 0;
  display: grid;
  gap: 2px;
}

.proxy-inventory-heading > div > span {
  display: block;
  color: var(--ui-primary);
  font: 700 var(--ui-font-caption)/1.25 "v-mono", ui-monospace, monospace;
}

.proxy-inventory-heading h2 {
  margin: 0;
  color: var(--ui-text);
  font-size: 16px;
  font-weight: 650;
  line-height: 20px;
}

.proxy-inventory-heading p {
  margin: 0;
  color: var(--ui-text-muted);
  font-size: var(--ui-font-body-sm);
  line-height: 16px;
}

.proxy-inventory-actions {
  display: flex;
  flex: 0 0 auto;
  gap: 8px;
}

.proxy-inventory-actions button {
  min-height: 36px;
}

.proxy-inventory-actions .el-icon {
  font-size: 16px;
}

@media (max-width: 900px) {
  .proxy-inventory-header {
    align-items: stretch;
    flex-direction: column;
  }

  .proxy-inventory-actions button {
    min-height: 44px;
    flex: 1;
  }
}

@media (max-width: 460px) {
  .proxy-inventory-heading {
    align-items: flex-start;
  }

  .proxy-inventory-heading .section-icon {
    display: none;
  }

  .proxy-inventory-heading p {
    min-height: 32px;
  }
}
</style>
