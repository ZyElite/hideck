<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type { CarrierQueryRule } from '../../types/commands'
import {
  Add24Regular,
  ArrowSync24Regular,
  Database24Regular,
  Delete24Regular,
  Edit24Regular,
  Save24Regular
} from '@vicons/fluent'
import { carrierReplySenderError } from '../../utils/commandInput'

const props = defineProps<{
  modelValue: boolean
  builtIn: CarrierQueryRule[]
  custom: CarrierQueryRule[]
  saving: boolean
  loading: boolean
  loaded: boolean
  error: string
  deletingId: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  save: [rule: CarrierQueryRule, updating: boolean]
  delete: [id: string]
  refresh: []
}>()

const activeTab = ref('custom')
const editingID = ref('')
const sendersText = ref('')
const limitationsText = ref('')
const submitAttempted = ref(false)
const form = reactive<CarrierQueryRule>(blankRule())
const isExisting = computed(() => props.custom.some((rule) => rule.id === editingID.value))
const mutationBusy = computed(() => props.saving || Boolean(props.deletingId))
const sendersError = computed(() => submitAttempted.value
  ? carrierReplySenderError(form.response_mode, sendersText.value)
  : '')
const formError = computed(() => submitAttempted.value ? validateRequiredFields() : '')

watch(() => props.modelValue, (open) => {
  if (open && !editingID.value) startNew()
})

watch(() => props.custom.map((rule) => rule.id).join('\n'), () => {
  if (editingID.value && !isExisting.value) startNew()
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
  if (formError.value || sendersError.value) return
  emit('save', {
    ...form,
    expected_senders: lines(sendersText.value),
    limitations: lines(limitationsText.value),
    built_in: false
  }, isExisting.value)
}

function validateRequiredFields(): string {
  if (!form.id.trim()) return '请填写规则 ID'
  if (!/^[A-Za-z0-9][A-Za-z0-9_.-]*$/.test(form.id.trim())) return '规则 ID 只能包含字母、数字、点、下划线和连字符'
  if (!form.operator.trim()) return '请填写运营商名称'
  if (!/^\d{3}$/.test(form.mcc.trim())) return 'MCC 必须是 3 位数字'
  if (!/^\d{2,3}$/.test(form.mnc.trim())) return 'MNC 必须是 2 或 3 位数字'
  if (form.transport === 'sms' && (!form.destination?.trim() || !form.payload?.trim())) return 'SMS 规则需要目标号码和查询内容'
  if (form.transport === 'ussd' && !form.payload?.trim()) return 'USSD 规则需要查询代码'
  if (form.transport === 'unsupported' && !form.alternative?.trim()) return '不支持自动查询时需要填写官方替代方式'
  if (form.transport === 'unsupported' && form.response_mode !== 'none') return '不支持的规则请选择“无自动查询”回复方式'
  return sendersError.value
}

function lines(value: string) {
  return value.split('\n').map((item) => item.trim()).filter(Boolean)
}
</script>

<template>
  <el-drawer
    :model-value="modelValue"
    class="command-rule-drawer"
    title="运营商规则管理"
    size="min(720px, 100%)"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <section class="source-banner" aria-label="规则数据来源">
      <el-icon aria-hidden="true"><Database24Regular /></el-icon>
      <div>
        <strong>实时后端规则库</strong>
        <span>服务端内置规则只读；自定义规则持久化到数据库</span>
      </div>
      <div class="source-summary">
        <small>内置 {{ loaded ? builtIn.length : '—' }}</small>
        <small>自定义 {{ loaded ? custom.length : '—' }}</small>
      </div>
      <el-button text :loading="loading" :disabled="mutationBusy" @click="emit('refresh')">
        <el-icon v-if="!loading"><ArrowSync24Regular /></el-icon><span>刷新</span>
      </el-button>
    </section>

    <div v-if="error" class="source-error" role="alert">
      <span>{{ error }}</span>
      <el-button text :disabled="loading || mutationBusy" @click="emit('refresh')">重新读取</el-button>
    </div>

    <el-tabs v-model="activeTab" class="rule-tabs">
      <el-tab-pane :label="`数据库自定义 ${custom.length}`" name="custom">
        <div class="inventory-heading">
          <div>
            <strong>自定义规则</strong>
            <span>可新建、编辑、删除；保存后会重新读取后端数据</span>
          </div>
          <el-button :disabled="mutationBusy" @click="startNew">
            <el-icon><Add24Regular /></el-icon>新建规则
          </el-button>
        </div>

        <div v-if="loading && !loaded" class="inventory-state">正在读取数据库自定义规则</div>
        <div v-else-if="!custom.length" class="inventory-state">数据库中暂无自定义规则</div>
        <div v-else class="custom-list" aria-label="数据库自定义规则列表">
          <article v-for="rule in custom" :key="rule.id" :class="{ selected: editingID === rule.id }">
            <button type="button" class="rule-select" @click="assignRule(rule)">
              <span>
                <strong>{{ rule.operator || rule.id }}</strong>
                <small>{{ rule.id }} · {{ rule.mcc }}/{{ rule.mnc }}</small>
              </span>
              <em :class="{ disabled: !rule.enabled }">{{ rule.enabled ? '已启用' : '已停用' }}</em>
            </button>
            <div class="row-actions">
              <el-button text :aria-label="`编辑 ${rule.id}`" :disabled="mutationBusy" @click="assignRule(rule)">
                <el-icon><Edit24Regular /></el-icon>
              </el-button>
              <el-button
                text
                type="danger"
                :aria-label="`删除 ${rule.id}`"
                :loading="deletingId === rule.id"
                :disabled="mutationBusy && deletingId !== rule.id"
                @click="emit('delete', rule.id)"
              >
                <el-icon v-if="deletingId !== rule.id"><Delete24Regular /></el-icon>
              </el-button>
            </div>
          </article>
        </div>

        <div class="editor-heading">
          <div>
            <span>{{ isExisting ? 'EDIT CUSTOM RULE' : 'CREATE CUSTOM RULE' }}</span>
            <h3>{{ isExisting ? `编辑 ${editingID}` : '新建自定义规则' }}</h3>
          </div>
          <el-button v-if="isExisting" text :disabled="mutationBusy" @click="startNew">退出编辑</el-button>
        </div>

        <el-form label-position="top" class="rule-form" @submit.prevent="submit">
          <div v-if="formError && !sendersError" class="form-error" role="alert">{{ formError }}</div>
          <div class="form-grid">
            <el-form-item label="规则 ID" required>
              <el-input v-model="form.id" :disabled="isExisting || mutationBusy" placeholder="carrier_variant" />
            </el-form-item>
            <el-form-item label="运营商" required><el-input v-model="form.operator" :disabled="mutationBusy" /></el-form-item>
            <el-form-item label="MCC" required><el-input v-model="form.mcc" :disabled="mutationBusy" maxlength="3" /></el-form-item>
            <el-form-item label="MNC" required><el-input v-model="form.mnc" :disabled="mutationBusy" maxlength="3" /></el-form-item>
            <el-form-item label="SPN 精确匹配"><el-input v-model="form.spn" :disabled="mutationBusy" /></el-form-item>
            <el-form-item label="规则变体"><el-input v-model="form.variant" :disabled="mutationBusy" /></el-form-item>
            <el-form-item label="传输方式" required>
              <el-select v-model="form.transport" :disabled="mutationBusy">
                <el-option label="SMS" value="sms" /><el-option label="USSD / USSI" value="ussd" />
                <el-option label="不支持自动查询" value="unsupported" />
              </el-select>
            </el-form-item>
            <el-form-item label="回复方式" required>
              <el-select v-model="form.response_mode" :disabled="mutationBusy">
                <el-option label="短信回复" value="sms" /><el-option label="直接回复" value="direct" />
                <el-option label="无自动查询" value="none" />
              </el-select>
            </el-form-item>
            <el-form-item label="目标号码"><el-input v-model="form.destination" :disabled="mutationBusy" /></el-form-item>
            <el-form-item label="查询内容 / 代码"><el-input v-model="form.payload" :disabled="mutationBusy" /></el-form-item>
            <el-form-item label="币种"><el-input v-model="form.currency" :disabled="mutationBusy" placeholder="GBP" /></el-form-item>
            <el-form-item label="资费状态"><el-input v-model="form.cost_status" :disabled="mutationBusy" /></el-form-item>
          </div>
          <el-form-item label="预期回复发送者（每行一个）" :required="form.response_mode === 'sms'" :error="sendersError">
            <el-input v-model="sendersText" type="textarea" :rows="2" :disabled="mutationBusy" />
          </el-form-item>
          <el-form-item label="余额解析正则（命名组 amount）"><el-input v-model="form.parser_pattern" type="textarea" :rows="2" :disabled="mutationBusy" /></el-form-item>
          <el-form-item label="证据类型"><el-input v-model="form.evidence_type" :disabled="mutationBusy" /></el-form-item>
          <el-form-item label="证据链接"><el-input v-model="form.evidence_url" :disabled="mutationBusy" /></el-form-item>
          <el-form-item label="限制说明（每行一条）"><el-input v-model="limitationsText" type="textarea" :rows="2" :disabled="mutationBusy" /></el-form-item>
          <el-form-item label="不支持时的替代方式"><el-input v-model="form.alternative" type="textarea" :rows="2" :disabled="mutationBusy" /></el-form-item>
          <el-form-item><el-switch v-model="form.enabled" :disabled="mutationBusy" active-text="启用规则" /></el-form-item>
          <div class="form-actions">
            <el-button
              v-if="isExisting"
              type="danger"
              plain
              :loading="deletingId === form.id"
              :disabled="mutationBusy && deletingId !== form.id"
              @click="emit('delete', form.id)"
            >
              <el-icon v-if="deletingId !== form.id"><Delete24Regular /></el-icon>删除
            </el-button>
            <el-button type="primary" native-type="submit" :loading="saving" :disabled="mutationBusy && !saving">
              <el-icon v-if="!saving"><Save24Regular /></el-icon>{{ isExisting ? '保存修改' : '创建规则' }}
            </el-button>
          </div>
        </el-form>
      </el-tab-pane>

      <el-tab-pane :label="`服务端内置 ${builtIn.length}`" name="builtin">
        <div class="readonly-note">
          <strong>服务端内置 · 只读</strong>
          <span>以下规则由后端内置规则集实时返回，不能在这里修改或删除；需要覆盖时请新建数据库自定义规则。</span>
        </div>
        <div v-if="loading && !loaded" class="inventory-state">正在读取服务端内置规则</div>
        <div v-else-if="!builtIn.length" class="inventory-state">后端当前未返回内置规则</div>
        <div v-else class="builtin-list">
          <article v-for="rule in builtIn" :key="rule.id">
            <div><strong>{{ rule.operator }}</strong><code>{{ rule.mcc }}/{{ rule.mnc }}</code></div>
            <p v-if="rule.transport !== 'unsupported'">{{ rule.transport.toUpperCase() }} · {{ rule.destination || rule.payload }} · {{ rule.payload }}</p>
            <p v-else>{{ rule.alternative }}</p>
            <a v-if="rule.evidence_url" :href="rule.evidence_url" target="_blank" rel="noreferrer">查看规则依据</a>
          </article>
        </div>
      </el-tab-pane>
    </el-tabs>
  </el-drawer>
</template>

<style scoped>
.source-banner { min-width: 0; margin-bottom: 12px; padding: 11px 0; border-block: 1px solid var(--ui-border); display: grid; grid-template-columns: auto minmax(0, 1fr) auto auto; align-items: center; gap: 10px; }
.rule-tabs :deep(.el-tabs__item) { min-height: 44px; }
:global(.command-rule-drawer .el-drawer__close-btn) { width: 44px; height: 44px; display: grid; place-items: center; }
.source-banner > .el-icon { color: var(--ui-primary); font-size: 20px; }
.source-banner strong, .source-banner span { display: block; }
.source-banner strong { color: var(--ui-text); font-size: 12px; }
.source-banner span { margin-top: 2px; color: var(--ui-text-subtle); font-size: 10px; }
.source-summary { display: flex; gap: 6px; }
.source-summary small { padding: 4px 6px; border: 1px solid var(--ui-border); border-radius: 3px; color: var(--ui-text-muted); font-size: 9px; white-space: nowrap; }
.source-banner :deep(.el-button) { min-height: 40px; color: var(--ui-primary); }
.source-error, .form-error { padding: 10px 12px; border: 1px solid color-mix(in srgb, var(--ui-danger) 38%, var(--ui-border)); background: color-mix(in srgb, var(--ui-danger) 7%, transparent); color: var(--ui-danger); font-size: 11px; }
.source-error { margin-bottom: 12px; display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.source-error :deep(.el-button) { color: var(--ui-danger); }
.inventory-heading, .editor-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.inventory-heading { padding: 4px 0 12px; }
.inventory-heading strong, .inventory-heading span { display: block; }
.inventory-heading strong { color: var(--ui-text); font-size: 13px; }
.inventory-heading span { margin-top: 3px; color: var(--ui-text-subtle); font-size: 10px; }
.inventory-heading :deep(.el-button) { min-height: 40px; }
.inventory-state { min-height: 88px; border-block: 1px solid var(--ui-border); color: var(--ui-text-subtle); display: grid; place-items: center; font-size: 11px; }
.custom-list { border-top: 1px solid var(--ui-border); }
.custom-list article { min-width: 0; min-height: 56px; border-bottom: 1px solid var(--ui-border); display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; transition: background-color 120ms ease; }
.custom-list article.selected { background: color-mix(in srgb, var(--ui-primary) 8%, transparent); box-shadow: inset 2px 0 var(--ui-primary); }
.rule-select { min-width: 0; min-height: 55px; padding: 8px 10px; border: 0; background: transparent; color: inherit; display: flex; align-items: center; justify-content: space-between; gap: 10px; text-align: left; cursor: pointer; }
.rule-select > span { min-width: 0; }
.rule-select strong, .rule-select small { display: block; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rule-select strong { color: var(--ui-text); font-size: 12px; }
.rule-select small { margin-top: 3px; color: var(--ui-text-subtle); font: 9px "v-mono", monospace; }
.rule-select em { padding: 3px 6px; border: 1px solid color-mix(in srgb, var(--ui-success) 42%, var(--ui-border)); border-radius: 999px; color: var(--ui-success); font-size: 9px; font-style: normal; white-space: nowrap; }
.rule-select em.disabled { border-color: var(--ui-border); color: var(--ui-text-subtle); }
.row-actions { padding-right: 3px; display: flex; }
.row-actions :deep(.el-button) { width: 40px; height: 40px; margin: 0; padding: 0; }
.editor-heading { margin-top: 22px; padding: 15px 0 11px; border-top: 1px solid var(--ui-border); }
.editor-heading span { color: var(--ui-primary); font: 8px/1.2 "v-mono", monospace; letter-spacing: .14em; }
.editor-heading h3 { margin: 4px 0 0; color: var(--ui-text); font-size: 15px; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0 14px; }
.form-error { margin-bottom: 14px; }
.rule-form :deep(.el-form-item) { margin-bottom: 15px; }
.rule-form :deep(.el-select) { width: 100%; }
.rule-form :deep(.el-input__wrapper), .rule-form :deep(.el-button) { min-height: 44px; }
.form-actions { position: sticky; bottom: 0; z-index: 2; padding: 12px 0; display: flex; justify-content: flex-end; gap: 8px; background: var(--el-bg-color); border-top: 1px solid var(--ui-border); }
.readonly-note { padding: 12px 0 14px; border-bottom: 1px solid var(--ui-border); }
.readonly-note strong, .readonly-note span { display: block; }
.readonly-note strong { color: var(--ui-primary); font-size: 12px; }
.readonly-note span { margin-top: 4px; color: var(--ui-text-subtle); font-size: 10px; line-height: 1.5; }
.builtin-list article { padding: 13px 2px; border-bottom: 1px solid var(--ui-border); }
.builtin-list article > div { display: flex; justify-content: space-between; gap: 12px; }
.builtin-list code, .builtin-list p { color: var(--ui-text-subtle); font-size: 11px; }
.builtin-list p { margin: 6px 0; line-height: 1.5; }
.builtin-list a { color: var(--ui-primary); font-size: 11px; }
@media (max-width: 560px) {
  .source-banner { grid-template-columns: auto minmax(0, 1fr) auto; }
  .source-summary { grid-column: 2 / -1; flex-wrap: wrap; }
  .source-banner :deep(.el-button), .inventory-heading :deep(.el-button), .source-error :deep(.el-button), .row-actions :deep(.el-button) { min-height: 44px; }
  .form-grid { grid-template-columns: 1fr; }
  .inventory-heading { align-items: flex-start; }
  .inventory-heading span { max-width: 210px; }
  .rule-select { min-height: 60px; padding-inline: 8px; }
  .row-actions :deep(.el-button) { width: 44px; height: 44px; }
}
@media (prefers-reduced-motion: reduce) { .custom-list article { transition: none; } }
</style>
