<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import type { DeviceMgmtListItem } from '../../types/api'
import type { BalanceQuery, ManualBalanceInput } from '../../types/commands'
import { prepareManualBalanceInput } from '../../utils/manualBalance'

const props = defineProps<{
  modelValue: boolean
  device?: DeviceMgmtListItem
  existing?: BalanceQuery
  saving: boolean
  clearing: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  save: [input: ManualBalanceInput]
  clear: []
}>()

const form = reactive({ amount: '', currency: '' })
const error = ref('')

watch(
  () => [props.modelValue, props.existing?.updated_at] as const,
  ([open]) => {
    if (!open) return
    form.amount = props.existing?.amount || ''
    form.currency = props.existing?.currency || ''
    error.value = ''
  }
)

function submit() {
  try {
    const input = prepareManualBalanceInput(form.amount, form.currency)
    error.value = ''
    emit('save', input)
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '手动余额格式无效'
  }
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    title="手动设置余额"
    width="min(440px, 92vw)"
    append-to-body
    :close-on-click-modal="!saving && !clearing"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div class="manual-balance-context">
      <strong>{{ device?.name || device?.id || '未选择设备' }}</strong>
      <span>{{ existing ? '当前已有手动余额，保存会更新原记录' : '用于无法通过短信或 USSD 查询的线路' }}</span>
    </div>
    <el-form label-position="top" @submit.prevent="submit">
      <div v-if="error" class="manual-balance-error" role="alert">{{ error }}</div>
      <el-form-item label="余额金额" required>
        <el-input v-model="form.amount" inputmode="decimal" placeholder="例如 12.89" :disabled="saving || clearing" />
      </el-form-item>
      <el-form-item label="币种">
        <el-input v-model="form.currency" maxlength="12" placeholder="例如 GBP、CNY 或 £" :disabled="saving || clearing" />
      </el-form-item>
      <p>保存后来源会明确显示为“手动录入”，不会触发短信或 USSD。</p>
    </el-form>
    <template #footer>
      <div class="manual-balance-actions">
        <el-button v-if="existing" type="danger" plain :loading="clearing" :disabled="saving" @click="emit('clear')">
          清除手动值
        </el-button>
        <span />
        <el-button :disabled="saving || clearing" @click="emit('update:modelValue', false)">取消</el-button>
        <el-button type="primary" :loading="saving" :disabled="clearing" @click="submit">保存余额</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<style scoped>
.manual-balance-context { margin-bottom: 16px; padding: 12px; border: 1px solid var(--ui-border); border-radius: 6px; background: var(--ui-surface-muted); }
.manual-balance-context strong, .manual-balance-context span { display: block; }
.manual-balance-context strong { color: var(--ui-text); font-size: var(--ui-font-body); }
.manual-balance-context span, p { margin: 4px 0 0; color: var(--ui-text-muted); font-size: var(--ui-font-body-sm); line-height: 1.5; }
.manual-balance-error { margin-bottom: 12px; padding: 9px 11px; border: 1px solid color-mix(in srgb, var(--ui-danger) 38%, var(--ui-border)); color: var(--ui-danger); font-size: var(--ui-font-body-sm); }
.manual-balance-actions { width: 100%; display: grid; grid-template-columns: auto 1fr auto auto; gap: 8px; }
.manual-balance-actions :deep(.el-button) { min-height: 42px; margin: 0; }
@media (max-width: 520px) {
  .manual-balance-actions { grid-template-columns: 1fr 1fr; }
  .manual-balance-actions > span { display: none; }
  .manual-balance-actions :deep(.el-button) { min-height: 44px; }
}
</style>
