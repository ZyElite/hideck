<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
  ArrowClockwise24Regular,
  CalendarClock24Regular,
  DeviceEq24Regular,
  Dismiss24Regular,
  Save24Regular,
  Sim24Regular
} from '@vicons/fluent'
import { devicesService } from '../services/devices'
import type { DeviceMgmtListItem } from '../types/api'
import type { AutomaticTask, AutomaticTaskInput, AutomaticTaskType } from '../types/automation'
import {
  automaticTaskProfileKey,
  automaticTaskProfileOption,
  automaticTaskToInput,
  currentAutomaticTaskICCID,
  defaultAutomaticTaskInput,
  flattenAutomaticTaskProfiles,
  mergeAutomaticTaskProfiles,
  validateAutomaticTaskInput
} from '../utils/automaticTaskEditor'
import type { AutomaticTaskProfileOption } from '../utils/automaticTaskEditor'

const props = defineProps<{
  modelValue: boolean
  task: AutomaticTask | null
  devices: DeviceMgmtListItem[]
  saving: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  submit: [value: AutomaticTaskInput]
}>()

const profileOptions = ref<AutomaticTaskProfileOption[]>([])
const profilesLoading = ref(false)
const deviceBackend = ref('')
const configLoading = ref(false)
const configError = ref('')
const formError = ref('')
const nameInput = ref<{ focus: () => void } | null>(null)
let profileRequest = 0
let configRequest = 0

const form = reactive<AutomaticTaskInput>(defaultAutomaticTaskInput())
const selectedProfile = ref('')
const isPCSC = computed(() => deviceBackend.value === 'pcsc')
const deviceConfigUnavailable = computed(() => Boolean(form.device_id)
  && (configLoading.value || Boolean(configError.value)))
const taskControlsDisabled = computed(() => props.saving || deviceConfigUnavailable.value)

watch(() => props.modelValue, (open) => {
  if (!open) {
    configRequest++
    configLoading.value = false
    configError.value = ''
    deviceBackend.value = ''
    return
  }
  formError.value = ''
  Object.assign(form, props.task ? automaticTaskToInput(props.task) : defaultAutomaticTaskInput())
  if (!form.device_id && props.devices.length) form.device_id = props.devices[0].id
  selectedProfile.value = automaticTaskProfileKey(form.profile_iccid, form.profile_aid || '')
  resetProfileOptions(form.device_id)
  void loadDeviceConfig(form.device_id)
  if (form.profile_aid) void loadESIMProfiles()
})

function changeDevice(deviceID: string) {
  formError.value = ''
  form.profile_iccid = ''
  form.profile_aid = ''
  selectedProfile.value = ''
  resetProfileOptions(deviceID)
  void loadDeviceConfig(deviceID)
}

watch(() => form.task_type, (taskType) => {
  if (taskType === 'call' && !form.payload.hold_seconds) form.payload.hold_seconds = 10
  if (taskType === 'public_ip') form.environment = 'cellular'
})

watch(isPCSC, (pcsc) => {
  if (!pcsc) return
  form.environment = 'vowifi'
  if (form.task_type === 'public_ip') form.task_type = 'sms'
})

async function loadDeviceConfig(deviceID: string) {
  const requestID = ++configRequest
  deviceBackend.value = ''
  configError.value = ''
  configLoading.value = Boolean(deviceID)
  if (!deviceID) return
  const config = await devicesService.getConfig(deviceID)
  if (requestID !== configRequest) return
  configLoading.value = false
  if (!config.ok) {
    configError.value = config.error.message || '设备配置读取失败'
    return
  }
  const backend = config.data?.device_backend
  if (!backend) {
    configError.value = '设备配置未返回后端类型'
    return
  }
  deviceBackend.value = backend
}

async function loadESIMProfiles() {
  const deviceID = form.device_id
  if (!deviceID) return
  const requestID = ++profileRequest
  profilesLoading.value = true
  const overview = await devicesService.getEsimOverview(deviceID)
  if (requestID !== profileRequest) return
  profilesLoading.value = false
  if (!overview.ok) {
    ElMessage.error(overview.error.message || 'eSIM profile 加载失败')
    return
  }
  profileOptions.value = mergeAutomaticTaskProfiles(profileOptions.value, flattenAutomaticTaskProfiles(overview.data.profiles))
  const current = automaticTaskProfileKey(form.profile_iccid, form.profile_aid || '')
  if (current && profileOptions.value.some((item) => item.key === current)) selectedProfile.value = current
}

function resetProfileOptions(deviceID: string) {
  profileRequest++
  profilesLoading.value = false
  const current = props.devices.find((device) => device.id === deviceID)
  const iccid = currentAutomaticTaskICCID(current)
  const options: AutomaticTaskProfileOption[] = []
  if (iccid) options.push(automaticTaskProfileOption(iccid, '', '当前 SIM'))
  if (form.profile_iccid) options.push(automaticTaskProfileOption(
    form.profile_iccid,
    form.profile_aid || '',
    form.profile_aid ? '任务 eSIM' : '任务 SIM'
  ))
  profileOptions.value = mergeAutomaticTaskProfiles([], options)
  if (!form.profile_iccid && iccid) {
    form.profile_iccid = iccid
    form.profile_aid = ''
  }
  selectedProfile.value = automaticTaskProfileKey(form.profile_iccid, form.profile_aid || '')
}

function applyProfile(key: string) {
  const option = profileOptions.value.find((item) => item.key === key)
  form.profile_iccid = option?.iccid || ''
  form.profile_aid = option?.aid || ''
}

function setTaskType(value: string | number | boolean | undefined) {
  const taskType = String(value) as AutomaticTaskType
  if (taskType === 'sms' || taskType === 'call' || taskType === 'public_ip') form.task_type = taskType
}

function submit() {
  if (props.saving) return
  if (deviceConfigUnavailable.value) {
    formError.value = configError.value || '正在读取设备配置'
    return
  }
  const error = validateAutomaticTaskInput(form)
  if (error) {
    formError.value = error
    ElMessage.warning(error)
    return
  }
  formError.value = ''
  emit('submit', {
    ...form,
    name: form.name.trim(),
    device_id: form.device_id.trim(),
    profile_iccid: form.profile_iccid.trim(),
    profile_aid: form.profile_aid?.trim(),
    timezone: form.timezone.trim(),
    payload: { ...form.payload, phone: form.payload.phone?.trim() }
  })
}

function handleOpenChange(open: boolean) {
  if (!open && props.saving) return
  emit('update:modelValue', open)
}

function focusNameInput() {
  nameInput.value?.focus()
}
</script>

<template>
  <el-drawer
    :model-value="modelValue"
    class="automatic-task-editor"
    modal-class="automatic-task-editor-scrim"
    direction="rtl"
    size="min(720px, 100vw)"
    append-to-body
    destroy-on-close
    :close-on-click-modal="!saving"
    :close-on-press-escape="!saving"
    :show-close="false"
    @update:model-value="handleOpenChange"
    @opened="focusNameInput"
  >
    <template #header>
      <header class="editor-header">
        <span class="editor-icon" aria-hidden="true"><el-icon><CalendarClock24Regular /></el-icon></span>
        <div>
          <span>TASK EDITOR</span>
          <h2>{{ task ? '编辑自动任务' : '新建自动任务' }}</h2>
        </div>
        <el-button circle text :disabled="saving" aria-label="关闭自动任务编辑器" @click="handleOpenChange(false)">
          <el-icon><Dismiss24Regular /></el-icon>
        </el-button>
      </header>
    </template>

    <el-form label-position="top" class="task-form" @submit.prevent="submit">
      <div v-if="formError" class="form-error" role="alert">{{ formError }}</div>

      <section class="form-section">
        <header><el-icon><DeviceEq24Regular /></el-icon><div><h3>任务与设备</h3><span>选择执行设备和绑定的 SIM / eSIM 配置</span></div></header>
        <div class="form-grid form-grid-identity">
          <el-form-item label="任务名称">
            <el-input ref="nameInput" v-model="form.name" maxlength="80" :disabled="saving" />
          </el-form-item>
          <el-form-item label="状态">
            <el-switch v-model="form.enabled" :disabled="saving" active-text="启用" inactive-text="停用" />
          </el-form-item>
          <el-form-item label="设备">
            <el-select v-model="form.device_id" filterable class="w-full" :disabled="saving" @change="changeDevice">
              <el-option v-for="device in devices" :key="device.id" :label="device.name || device.id" :value="device.id" />
            </el-select>
          </el-form-item>
          <el-form-item label="SIM / eSIM Profile">
            <div class="profile-selector">
              <el-select v-model="selectedProfile" filterable class="w-full" :loading="profilesLoading" :disabled="saving" @change="applyProfile">
                <el-option v-for="profile in profileOptions" :key="profile.key" :label="profile.label" :value="profile.key" />
              </el-select>
              <el-tooltip content="读取 eSIM profiles">
                <el-button :loading="profilesLoading" :disabled="saving || !form.device_id" aria-label="读取 eSIM profiles" @click="loadESIMProfiles">
                  <el-icon v-if="!profilesLoading"><ArrowClockwise24Regular /></el-icon><span>读取</span>
                </el-button>
              </el-tooltip>
            </div>
          </el-form-item>
        </div>
      </section>

      <section class="form-section">
        <header><el-icon><Sim24Regular /></el-icon><div><h3>执行内容</h3><span>任务能力由当前设备后端和任务类型共同决定</span></div></header>
        <div v-if="configError" class="config-state config-error" role="alert">
          <span>{{ configError }}</span>
          <el-button text :disabled="configLoading || saving" @click="loadDeviceConfig(form.device_id)">重新读取</el-button>
        </div>
        <div v-else-if="configLoading" class="config-state" role="status">正在读取设备能力</div>
        <el-form-item label="任务类型">
          <el-radio-group :model-value="form.task_type" :disabled="taskControlsDisabled" @update:model-value="setTaskType">
            <el-radio-button value="sms">短信</el-radio-button>
            <el-radio-button value="call">通话</el-radio-button>
            <el-radio-button value="public_ip" :disabled="isPCSC">公网 IP</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <el-form-item v-if="form.task_type !== 'public_ip'" label="运行环境">
          <el-radio-group v-model="form.environment" :disabled="taskControlsDisabled">
            <el-radio-button value="vowifi">VoWiFi</el-radio-button>
            <el-radio-button value="cellular" :disabled="isPCSC">蜂窝</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <div v-if="form.task_type === 'sms'" class="form-grid">
          <el-form-item label="接收号码"><el-input v-model="form.payload.phone" maxlength="32" :disabled="taskControlsDisabled" /></el-form-item>
          <el-form-item label="短信内容"><el-input v-model="form.payload.message" maxlength="1000" :disabled="taskControlsDisabled" /></el-form-item>
        </div>
        <div v-if="form.task_type === 'call'" class="form-grid">
          <el-form-item label="呼叫号码"><el-input v-model="form.payload.phone" maxlength="32" :disabled="taskControlsDisabled" /></el-form-item>
          <el-form-item label="保持时长（秒）"><el-input-number v-model="form.payload.hold_seconds" :min="1" :max="60" :disabled="taskControlsDisabled" class="w-full" /></el-form-item>
        </div>
      </section>

      <section class="form-section">
        <header><el-icon><CalendarClock24Regular /></el-icon><div><h3>运行计划</h3><span>按设备所在业务时区计算每次计划时间</span></div></header>
        <div class="form-grid form-grid-schedule">
          <el-form-item label="起始日期"><el-date-picker v-model="form.start_date" type="date" value-format="YYYY-MM-DD" :disabled="saving" class="w-full" /></el-form-item>
          <el-form-item label="执行时间"><el-time-picker v-model="form.run_time" format="HH:mm" value-format="HH:mm" :disabled="saving" class="w-full" /></el-form-item>
          <el-form-item label="间隔天数"><el-input-number v-model="form.interval_days" :min="1" :max="365" :disabled="saving" class="w-full" /></el-form-item>
          <el-form-item label="时区">
            <el-select v-model="form.timezone" filterable allow-create :disabled="saving" class="w-full">
              <el-option label="Asia/Shanghai" value="Asia/Shanghai" /><el-option label="Europe/London" value="Europe/London" /><el-option label="UTC" value="UTC" />
            </el-select>
          </el-form-item>
          <el-form-item label="失败重试"><el-input-number v-model="form.retry_count" :min="0" :max="10" :disabled="saving" class="w-full" /></el-form-item>
          <el-form-item label="完成通知"><el-switch v-model="form.notify" :disabled="saving" active-text="发送" inactive-text="关闭" /></el-form-item>
        </div>
      </section>
    </el-form>
    <template #footer>
      <div class="editor-footer">
        <span>{{ task ? '保存后更新现有计划' : '保存后创建新的自动任务' }}</span>
        <div>
          <el-button :disabled="saving" @click="handleOpenChange(false)">取消</el-button>
          <el-button type="primary" :loading="saving" :disabled="saving || deviceConfigUnavailable" @click="submit"><el-icon v-if="!saving"><Save24Regular /></el-icon>保存任务</el-button>
        </div>
      </div>
    </template>
  </el-drawer>
</template>

<style scoped>
:global(.automatic-task-editor-scrim) { background: color-mix(in srgb, #000 54%, transparent); }
:global(.automatic-task-editor) { border-radius: 8px 0 0 8px; background: var(--ui-surface); }
:global(.automatic-task-editor .el-drawer__header) { min-height: 70px; margin: 0; padding: 12px 18px; border-bottom: 1px solid var(--ui-border); color: var(--ui-text); }
:global(.automatic-task-editor .el-drawer__body) { min-height: 0; padding: 16px 18px 0; overflow-y: auto; }
:global(.automatic-task-editor .el-drawer__footer) { padding: 0 18px; border-top: 1px solid var(--ui-border); }
.editor-header { width: 100%; min-width: 0; display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 10px; }
.editor-icon { width: 38px; height: 38px; border: 1px solid color-mix(in srgb, var(--ui-primary) 42%, var(--ui-border)); border-radius: 6px; background: color-mix(in srgb, var(--ui-primary) 9%, transparent); color: var(--ui-primary); display: grid; place-items: center; }
.editor-header div > span { color: var(--ui-primary); font: var(--ui-font-caption)/1.2 "v-mono", monospace; letter-spacing: .14em; }
.editor-header h2 { margin: 4px 0 0; color: var(--ui-text); font-size: 18px; }
.editor-header :deep(.el-button) { width: 44px; height: 44px; }
.form-error { margin-bottom: 14px; padding: 10px 12px; border: 1px solid color-mix(in srgb, var(--ui-danger) 40%, var(--ui-border)); border-radius: 4px; background: color-mix(in srgb, var(--ui-danger) 7%, transparent); color: var(--ui-danger); font-size: var(--ui-font-body-sm); }
.config-state { min-height: 44px; margin-bottom: 14px; padding: 8px 10px; border: 1px solid var(--ui-border); border-radius: 4px; background: color-mix(in srgb, var(--ui-primary) 5%, transparent); color: var(--ui-text-muted); display: flex; align-items: center; justify-content: space-between; gap: 10px; font-size: var(--ui-font-body-sm); }
.config-error { border-color: color-mix(in srgb, var(--ui-danger) 40%, var(--ui-border)); background: color-mix(in srgb, var(--ui-danger) 7%, transparent); color: var(--ui-danger); }
.config-state :deep(.el-button) { flex: 0 0 auto; }
.form-section { padding: 14px 0 2px; border-bottom: 1px solid var(--ui-border); }
.form-section:last-child { border-bottom: 0; }
.form-section > header { margin-bottom: 14px; display: flex; align-items: flex-start; gap: 9px; }
.form-section > header > .el-icon { margin-top: 2px; color: var(--ui-primary); font-size: 18px; }
.form-section h3, .form-section header span { display: block; }
.form-section h3 { margin: 0; color: var(--ui-text); font-size: 15px; }
.form-section header span { margin-top: 3px; color: var(--ui-text-subtle); font-size: var(--ui-font-body-sm); }
.task-form :deep(.el-form-item) { margin-bottom: 15px; }
.task-form :deep(.el-input__wrapper), .task-form :deep(.el-button), .task-form :deep(.el-radio-button__inner) { min-height: 44px; }
.task-form :deep(.el-select) { width: 100%; }
.profile-selector { width: 100%; display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 8px; }
.form-grid { display: grid; grid-template-columns: minmax(0, 2fr) minmax(160px, 1fr); gap: 0 16px; }
.form-grid-identity { grid-template-columns: minmax(0, 1fr) minmax(160px, .5fr); }
.form-grid-schedule { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.editor-footer { min-height: 70px; display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.editor-footer > span { color: var(--ui-text-subtle); font-size: var(--ui-font-body-sm); }
.editor-footer > div { display: flex; gap: 8px; }
.editor-footer :deep(.el-button) { min-height: 44px; margin: 0; }
@media (max-width: 640px) {
  :global(.automatic-task-editor) { border-radius: 0; }
  :global(.automatic-task-editor .el-drawer__body) { padding-inline: 14px; }
  :global(.automatic-task-editor .el-drawer__footer) { padding-bottom: env(safe-area-inset-bottom); }
  .form-grid, .form-grid-schedule { grid-template-columns: minmax(0, 1fr); }
  .task-form :deep(.el-switch) { min-width: 44px; min-height: 44px; justify-content: center; }
  .editor-footer { padding: 10px 0; align-items: stretch; flex-direction: column; }
  .editor-footer > div { display: grid; grid-template-columns: 1fr 1fr; }
}
@media (prefers-reduced-motion: reduce) {
  :global(.automatic-task-editor), :global(.automatic-task-editor-scrim) { animation: none !important; transition-duration: 120ms !important; }
}
</style>
