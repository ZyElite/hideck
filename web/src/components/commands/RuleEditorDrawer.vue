<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type { CarrierQueryRule } from '../../types/commands'
import { Add24Regular, Delete24Regular, Save24Regular } from '@vicons/fluent'
import { carrierReplySenderError } from '../../utils/commandInput'

const props = defineProps<{
  modelValue: boolean
  builtIn: CarrierQueryRule[]
  custom: CarrierQueryRule[]
  saving: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  save: [rule: CarrierQueryRule]
  delete: [id: string]
}>()

const activeTab = ref('custom')
const editingID = ref('')
const sendersText = ref('')
const limitationsText = ref('')
const submitAttempted = ref(false)
const form = reactive<CarrierQueryRule>(blankRule())
const isExisting = computed(() => props.custom.some((rule) => rule.id === editingID.value))
const sendersError = computed(() => submitAttempted.value ? carrierReplySenderError(form.response_mode, sendersText.value) : '')

watch(() => props.modelValue, (open) => {
  if (open && !editingID.value) startNew()
})

function blankRule(): CarrierQueryRule {
  return {
    id: '', mcc: '', mnc: '', operator: '', transport: 'sms', destination: '', payload: '',
    response_mode: 'sms', expected_senders: [], parser_pattern: '', currency: '', cost_status: 'unknown',
    evidence_type: 'user', evidence_url: '', limitations: [], alternative: '', enabled: true, built_in: false
  }
}

function assignRule(rule: CarrierQueryRule) {
  Object.assign(form, blankRule(), rule, {
    expected_senders: [...(rule.expected_senders || [])],
    limitations: [...(rule.limitations || [])],
    built_in: false
  })
  editingID.value = rule.id
  sendersText.value = (rule.expected_senders || []).join('\n')
  limitationsText.value = (rule.limitations || []).join('\n')
  submitAttempted.value = false
}

function startNew() {
  Object.assign(form, blankRule())
  editingID.value = ''
  sendersText.value = ''
  limitationsText.value = ''
  submitAttempted.value = false
}

function submit() {
  submitAttempted.value = true
  if (sendersError.value) return
  emit('save', {
    ...form,
    expected_senders: lines(sendersText.value),
    limitations: lines(limitationsText.value),
    built_in: false
  })
}

function lines(value: string) {
  return value.split('\n').map((item) => item.trim()).filter(Boolean)
}
</script>

<template>
  <el-drawer
    :model-value="modelValue"
    title="运营商余额规则"
    size="min(680px, 100%)"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <el-tabs v-model="activeTab" class="rule-tabs">
      <el-tab-pane label="自定义覆盖" name="custom">
        <div class="editor-toolbar">
          <el-select
            :model-value="editingID"
            clearable
            placeholder="选择已有规则"
            @update:model-value="(value) => value ? assignRule(custom.find((item) => item.id === value)!) : startNew()"
          >
            <el-option v-for="rule in custom" :key="rule.id" :label="`${rule.operator} · ${rule.id}`" :value="rule.id" />
          </el-select>
          <el-button @click="startNew"><el-icon><Add24Regular /></el-icon>新建</el-button>
        </div>

        <el-form label-position="top" class="rule-form" @submit.prevent="submit">
          <div class="form-grid">
            <el-form-item label="规则 ID" required>
              <el-input v-model="form.id" :disabled="isExisting" placeholder="carrier_variant" />
            </el-form-item>
            <el-form-item label="运营商" required><el-input v-model="form.operator" /></el-form-item>
            <el-form-item label="MCC" required><el-input v-model="form.mcc" maxlength="3" /></el-form-item>
            <el-form-item label="MNC" required><el-input v-model="form.mnc" maxlength="3" /></el-form-item>
            <el-form-item label="SPN 精确匹配"><el-input v-model="form.spn" /></el-form-item>
            <el-form-item label="规则变体"><el-input v-model="form.variant" /></el-form-item>
            <el-form-item label="传输方式" required>
              <el-select v-model="form.transport">
                <el-option label="SMS" value="sms" /><el-option label="USSD / USSI" value="ussd" />
                <el-option label="不支持" value="unsupported" />
              </el-select>
            </el-form-item>
            <el-form-item label="回复方式" required>
              <el-select v-model="form.response_mode">
                <el-option label="短信回复" value="sms" /><el-option label="直接回复" value="direct" />
                <el-option label="无自动查询" value="none" />
              </el-select>
            </el-form-item>
            <el-form-item label="目标号码"><el-input v-model="form.destination" /></el-form-item>
            <el-form-item label="查询内容 / 代码"><el-input v-model="form.payload" /></el-form-item>
            <el-form-item label="币种"><el-input v-model="form.currency" placeholder="GBP" /></el-form-item>
            <el-form-item label="资费状态"><el-input v-model="form.cost_status" /></el-form-item>
          </div>
          <el-form-item
            label="预期回复发送者（每行一个）"
            :required="form.response_mode === 'sms'"
            :error="sendersError"
          >
            <el-input v-model="sendersText" type="textarea" :rows="2" />
          </el-form-item>
          <el-form-item label="余额解析正则（命名组 amount）"><el-input v-model="form.parser_pattern" type="textarea" :rows="2" /></el-form-item>
          <el-form-item label="证据类型"><el-input v-model="form.evidence_type" /></el-form-item>
          <el-form-item label="证据链接"><el-input v-model="form.evidence_url" /></el-form-item>
          <el-form-item label="限制说明（每行一条）"><el-input v-model="limitationsText" type="textarea" :rows="2" /></el-form-item>
          <el-form-item label="不支持时的替代方式"><el-input v-model="form.alternative" type="textarea" :rows="2" /></el-form-item>
          <el-form-item><el-switch v-model="form.enabled" active-text="启用规则" /></el-form-item>
          <div class="form-actions">
            <el-button v-if="isExisting" type="danger" plain @click="emit('delete', form.id)">
              <el-icon><Delete24Regular /></el-icon>删除
            </el-button>
            <el-button type="primary" native-type="submit" :loading="saving">
              <el-icon><Save24Regular /></el-icon>保存
            </el-button>
          </div>
        </el-form>
      </el-tab-pane>

      <el-tab-pane :label="`内置规则 ${builtIn.length}`" name="builtin">
        <div class="builtin-list">
          <article v-for="rule in builtIn" :key="rule.id">
            <div><strong>{{ rule.operator }}</strong><code>{{ rule.mcc }}/{{ rule.mnc }}</code></div>
            <p v-if="rule.transport !== 'unsupported'">{{ rule.transport.toUpperCase() }} · {{ rule.destination || rule.payload }} · {{ rule.payload }}</p>
            <p v-else>{{ rule.alternative }}</p>
            <a v-if="rule.evidence_url" :href="rule.evidence_url" target="_blank" rel="noreferrer">查看依据</a>
          </article>
        </div>
      </el-tab-pane>
    </el-tabs>
  </el-drawer>
</template>

<style scoped>
.editor-toolbar { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 8px; margin-bottom: 18px; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0 14px; }
.rule-form :deep(.el-form-item) { margin-bottom: 15px; }
.rule-form :deep(.el-select) { width: 100%; }
.rule-form :deep(.el-input__wrapper), .rule-form :deep(.el-button) { min-height: 44px; }
.form-actions { position: sticky; bottom: 0; padding: 12px 0; display: flex; justify-content: flex-end; gap: 8px; background: var(--el-bg-color); border-top: 1px solid var(--ui-border); }
.builtin-list article { padding: 13px 2px; border-bottom: 1px solid var(--ui-border); }
.builtin-list article > div { display: flex; justify-content: space-between; gap: 12px; }
.builtin-list code { color: #64748b; font-size: 12px; }
.builtin-list p { margin: 6px 0; color: #64748b; font-size: 12px; line-height: 1.5; }
.builtin-list a { color: #0f766e; font-size: 12px; }
@media (max-width: 560px) { .form-grid { grid-template-columns: 1fr; } }
</style>
