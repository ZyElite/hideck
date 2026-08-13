<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import BalancePanel from './BalancePanel.vue'
import type { DeviceMgmtListItem } from '../../types/api'
import type { BalanceQuery, CarrierQueryRule } from '../../types/commands'

const props = defineProps<{
  modelValue: boolean
  devices: DeviceMgmtListItem[]
  selectedDevice: string
  queries: BalanceQuery[]
  builtInRules: CarrierQueryRule[]
  customRules: CarrierQueryRule[]
  loading: boolean
  querying: boolean
  manualBalanceOpening: boolean
  rulesLoading: boolean
  rulesLoaded: boolean
  rulesError: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  'update:selectedDevice': [value: string]
  query: []
  editManualBalance: []
  editRules: []
  editRule: [rule: CarrierQueryRule]
  refreshRules: []
}>()

const rail = ref<HTMLElement | null>(null)

watch(() => props.modelValue, async (open) => {
  if (!open) return
  await nextTick()
  const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  rail.value?.scrollIntoView({ behavior: reduceMotion ? 'auto' : 'smooth', block: 'nearest' })
  rail.value?.focus({ preventScroll: true })
  emit('update:modelValue', false)
})
</script>

<template>
  <aside id="command-balance-rail" ref="rail" class="balance-rail" tabindex="-1">
    <BalancePanel
      :selected-device="selectedDevice"
      :devices="devices"
      :queries="queries"
      :built-in-rules="builtInRules"
      :custom-rules="customRules"
      :loading="loading"
      :querying="querying"
      :manual-balance-opening="manualBalanceOpening"
      :rules-loading="rulesLoading"
      :rules-loaded="rulesLoaded"
      :rules-error="rulesError"
      @update:selected-device="emit('update:selectedDevice', $event)"
      @query="emit('query')"
      @edit-manual-balance="emit('editManualBalance')"
      @edit-rules="emit('editRules')"
      @edit-rule="emit('editRule', $event)"
      @refresh-rules="emit('refreshRules')"
    />
  </aside>
</template>

<style scoped>
.balance-rail {
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  border-left: 1px solid var(--ui-border);
  background: color-mix(in srgb, var(--ui-surface-strong) 72%, var(--ui-surface));
  outline: none;
}
.balance-rail:focus-visible { box-shadow: inset 0 0 0 2px color-mix(in srgb, var(--ui-primary) 34%, transparent); }
@media (max-width: 1023px) {
  .balance-rail { min-height: 560px; border-top: 1px solid var(--ui-border); border-left: 0; }
}
</style>
