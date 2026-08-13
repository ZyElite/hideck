<script setup lang="ts">
import type { BalanceQuery } from '../../types/commands'
import {
  balanceResultText,
  balanceTransportLabel,
  presentBalanceState
} from '../../utils/commandPresentation'

defineProps<{ query: BalanceQuery }>()
</script>

<template>
  <div class="balance-message" :class="`tone-${presentBalanceState(query).tone}`">
    <div class="balance-result-row">
      <strong>{{ balanceResultText(query) }}</strong>
      <span>{{ presentBalanceState(query).label }}</span>
    </div>
    <span class="balance-device">{{ query.device_id }} · {{ balanceTransportLabel(query) }}</span>
    <pre v-if="query.raw_response">{{ query.raw_response }}</pre>
    <p v-if="query.error">{{ query.error }}</p>
  </div>
</template>

<style scoped>
.balance-message { min-width: 0; padding-top: 5px; display: grid; gap: 6px; color: var(--ui-success); }
.balance-result-row { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; }
.balance-result-row strong { color: currentColor; font-size: 16px; overflow-wrap: anywhere; }
.balance-result-row span { color: currentColor; font-size: var(--ui-font-caption); }
.balance-device { color: var(--ui-text-subtle); font: var(--ui-font-caption) "v-mono", monospace; }
.balance-message pre { margin: 1px 0 0; padding-top: 7px; border-top: 1px solid var(--ui-border); color: var(--ui-text-muted); font: var(--ui-font-body-sm)/1.5 "v-mono", monospace; white-space: pre-wrap; overflow-wrap: anywhere; }
.balance-message p { margin: 0; color: var(--ui-danger); font-size: var(--ui-font-body-sm); overflow-wrap: anywhere; }
.tone-waiting, .tone-running { color: var(--ui-warning); }
.tone-parsed { color: var(--ui-info); }
.tone-success { color: var(--ui-success); }
.tone-danger { color: var(--ui-danger); }
</style>
