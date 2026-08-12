<script setup lang="ts">
import { WeatherMoon24Regular, WeatherSunny24Regular } from '@vicons/fluent'

const props = defineProps<{
  isDark: boolean
}>()

const emit = defineEmits<{
  toggle: [next: boolean]
}>()

function onToggle() {
  emit('toggle', !props.isDark)
}
</script>

<template>
  <el-tooltip :content="isDark ? '切换到浅色模式' : '切换到深色模式'" placement="bottom">
    <el-button
      circle
      class="theme-toggle"
      :aria-label="isDark ? '切换到浅色模式' : '切换到深色模式'"
      @click="onToggle"
    >
      <el-icon v-if="isDark"><WeatherSunny24Regular /></el-icon>
      <el-icon v-else><WeatherMoon24Regular /></el-icon>
    </el-button>
  </el-tooltip>
</template>

<style scoped>
.theme-toggle {
  width: 38px;
  height: 38px;
  border-color: var(--ui-border);
  background: color-mix(in srgb, var(--ui-surface-muted) 82%, transparent);
  color: var(--ui-text-muted);
}

.theme-toggle:hover {
  border-color: color-mix(in srgb, var(--ui-primary) 48%, var(--ui-border));
  color: var(--ui-primary);
}
</style>
