<script setup lang="ts">
import type { BalanceQuery } from '../../types/commands'
import { balanceResultText, presentBalanceState } from '../../utils/commandPresentation'
import { Wallet24Regular } from '@vicons/fluent'

defineProps<{ query: BalanceQuery }>()

</script>

<template>
  <div class="balance-message" :class="query.state">
    <div class="balance-heading">
      <span><el-icon><Wallet24Regular /></el-icon>运营商余额</span>
      <span class="balance-state">{{ presentBalanceState(query).label }}</span>
    </div>
    <strong>{{ balanceResultText(query) }}</strong>
    <span class="balance-device">{{ query.device_id }}</span>
    <pre v-if="query.raw_response">{{ query.raw_response }}</pre>
    <p v-if="query.error">{{ query.error }}</p>
  </div>
</template>

<style scoped>
.balance-message { min-width: min(320px, 72vw); padding: 12px 14px; border: 1px solid rgba(13, 148, 136, .28); border-radius: 8px; background: rgba(13, 148, 136, .07); display: grid; gap: 7px; }
.balance-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; color: #0f766e; font-size: 12px; }
.balance-heading > span:first-child { display: inline-flex; align-items: center; gap: 6px; font-weight: 600; }
.balance-state { color: #64748b; font-size: 11px; }
.balance-message strong { font-size: 14px; overflow-wrap: anywhere; }
.balance-device { color: #64748b; font: 11px "v-mono", monospace; }
.balance-message pre { margin: 1px 0 0; padding-top: 8px; border-top: 1px solid var(--ui-border); font: 12px/1.5 "v-mono", monospace; white-space: pre-wrap; overflow-wrap: anywhere; }
.balance-message p { margin: 0; color: #dc2626; font-size: 12px; overflow-wrap: anywhere; }
.balance-message.failed, .balance-message.timed_out { border-color: rgba(239, 68, 68, .35); background: rgba(239, 68, 68, .07); }
.balance-message.failed .balance-heading, .balance-message.timed_out .balance-heading { color: #dc2626; }
</style>
