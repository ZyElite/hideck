<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import BalancePanel from './BalancePanel.vue'
import type { DeviceMgmtListItem } from '../../types/api'
import type { BalanceQuery } from '../../types/commands'

defineProps<{
  modelValue: boolean
  devices: DeviceMgmtListItem[]
  selectedDevice: string
  queries: BalanceQuery[]
  loading: boolean
  querying: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  'update:selectedDevice': [value: string]
  query: []
  editRules: []
}>()

const narrowViewport = ref(false)
const direction = computed<'btt' | 'rtl'>(() => narrowViewport.value ? 'btt' : 'rtl')
const size = computed(() => narrowViewport.value ? '70%' : '400px')
let mediaQuery: MediaQueryList | null = null

onMounted(() => {
  mediaQuery = window.matchMedia('(max-width: 640px)')
  narrowViewport.value = mediaQuery.matches
  mediaQuery.addEventListener('change', updateViewport)
})

onUnmounted(() => mediaQuery?.removeEventListener('change', updateViewport))

function updateViewport(event: MediaQueryListEvent) {
  narrowViewport.value = event.matches
}
</script>

<template>
  <el-drawer
    :model-value="modelValue"
    class="balance-drawer"
    :with-header="false"
    :direction="direction"
    :size="size"
    append-to-body
    @update:model-value="emit('update:modelValue', $event)"
  >
    <BalancePanel
      :selected-device="selectedDevice"
      :devices="devices"
      :queries="queries"
      :loading="loading"
      :querying="querying"
      @update:selected-device="emit('update:selectedDevice', $event)"
      @query="emit('query')"
      @edit-rules="emit('editRules')"
    />
  </el-drawer>
</template>

<style>
.balance-drawer .el-drawer__body { padding: 0; overflow: hidden; }
</style>
