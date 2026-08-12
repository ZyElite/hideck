<script setup lang="ts">
defineProps<{
  rows?: number
}>()
</script>

<template>
  <div class="ui-surface rounded-md p-4">
    <el-skeleton :rows="rows || 6" animated />
  </div>
</template>
