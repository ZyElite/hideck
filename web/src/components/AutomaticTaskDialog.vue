<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { devicesService } from '../services/devices'
import type { DeviceMgmtListItem, EsimEUICCProfiles } from '../types/api'
import type { AutomaticTask, AutomaticTaskInput, AutomaticTaskType } from '../types/automation'

type ProfileOption = {
  key: string
  iccid: string
  aid: string
  label: string
}

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

const profileOptions = ref<ProfileOption[]>([])
const profilesLoading = ref(false)
const deviceBackend = ref('')
let profileRequest = 0

const form = reactive<AutomaticTaskInput>(defaultTask())
const selectedProfile = ref('')
const isPCSC = computed(() => deviceBackend.value === 'pcsc')

watch(() => props.modelValue, (open) => {
  if (!open) return
  Object.assign(form, props.task ? taskToInput(props.task) : defaultTask())
  if (!form.device_id && props.devices.length) form.device_id = props.devices[0].id
  selectedProfile.value = profileKey(form.profile_iccid, form.profile_aid || '')
  void loadProfiles(form.device_id)
})

function changeDevice(deviceID: string) {
  form.profile_iccid = ''
  form.profile_aid = ''
  selectedProfile.value = ''
  void loadProfiles(deviceID)
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

function defaultTask(): AutomaticTaskInput {
  const now = new Date()
  const localDate = [now.getFullYear(), now.getMonth() + 1, now.getDate()]
    .map((value, index) => index === 0 ? String(value) : String(value).padStart(2, '0'))
    .join('-')
  return {
    name: '', enabled: true, device_id: '', profile_iccid: '', profile_aid: '',
    task_type: 'sms', environment: 'vowifi', interval_days: 1,
    start_date: localDate, run_time: '09:00',
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC',
    payload: { phone: '', message: '', hold_seconds: 10 }, retry_count: 0, notify: true
  }
}

function taskToInput(task: AutomaticTask): AutomaticTaskInput {
  return {
    name: task.name, enabled: task.enabled, device_id: task.device_id,
    profile_iccid: task.profile_iccid, profile_aid: task.profile_aid,
    task_type: task.task_type, environment: task.environment,
    interval_days: task.interval_days, start_date: task.start_date,
    run_time: task.run_time, timezone: task.timezone,
    payload: { ...task.payload }, retry_count: task.retry_count, notify: task.notify
  }
}

async function loadProfiles(deviceID: string) {
  const requestID = ++profileRequest
  profileOptions.value = []
  deviceBackend.value = ''
  if (!deviceID) return
  profilesLoading.value = true
  const [overview, config] = await Promise.all([
    devicesService.getEsimOverview(deviceID),
    devicesService.getConfig(deviceID)
  ])
  if (requestID !== profileRequest) return
  profilesLoading.value = false
  if (config.ok) deviceBackend.value = config.data?.device_backend || ''
  if (!overview.ok) {
    ElMessage.error(overview.error.message || 'eSIM profile 加载失败')
    return
  }
  profileOptions.value = flattenProfiles(overview.data.profiles)
  const current = profileKey(form.profile_iccid, form.profile_aid || '')
  if (current && profileOptions.value.some((item) => item.key === current)) selectedProfile.value = current
}

function flattenProfiles(groups: EsimEUICCProfiles[]): ProfileOption[] {
  return groups.flatMap((group) => group.profiles.map((profile) => ({
    key: profileKey(profile.iccid, group.aid_hex),
    iccid: profile.iccid,
    aid: group.aid_hex,
    label: `${profile.name || profile.service_provider_name || 'eSIM'} · ${profile.iccid}`
  })))
}

function profileKey(iccid: string, aid: string) {
  return `${iccid}|${aid}`
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
  const error = validateForm()
  if (error) {
    ElMessage.warning(error)
    return
  }
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

function validateForm() {
  if (!form.name.trim()) return '请输入任务名称'
  if (!form.device_id) return '请选择设备'
  if (!form.profile_iccid) return '请选择 eSIM profile'
  if (!form.start_date || !form.run_time || !form.timezone.trim()) return '请填写完整执行时间与时区'
  if (form.task_type === 'sms' && (!form.payload.phone?.trim() || !form.payload.message?.trim())) return '请填写短信号码和内容'
  if (form.task_type === 'call' && !form.payload.phone?.trim()) return '请填写通话号码'
  return ''
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    :title="task ? '编辑自动任务' : '新建自动任务'"
    width="min(640px, 94vw)"
    destroy-on-close
    @update:model-value="emit('update:modelValue', $event)"
  >
    <el-form label-position="top" class="task-form">
      <div class="form-grid">
        <el-form-item label="任务名称">
          <el-input v-model="form.name" maxlength="80" />
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.enabled" active-text="启用" inactive-text="停用" />
        </el-form-item>
        <el-form-item label="设备">
          <el-select v-model="form.device_id" filterable class="w-full" @change="changeDevice">
            <el-option v-for="device in devices" :key="device.id" :label="device.name || device.id" :value="device.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="eSIM Profile">
          <el-select
            v-model="selectedProfile"
            filterable
            class="w-full"
            :loading="profilesLoading"
            @change="applyProfile"
          >
            <el-option v-for="profile in profileOptions" :key="profile.key" :label="profile.label" :value="profile.key" />
          </el-select>
        </el-form-item>
      </div>

      <el-form-item label="任务类型">
        <el-radio-group :model-value="form.task_type" @update:model-value="setTaskType">
          <el-radio-button value="sms">短信</el-radio-button>
          <el-radio-button value="call">通话</el-radio-button>
          <el-radio-button value="public_ip" :disabled="isPCSC">公网 IP</el-radio-button>
        </el-radio-group>
      </el-form-item>

      <el-form-item v-if="form.task_type !== 'public_ip'" label="运行环境">
        <el-radio-group v-model="form.environment">
          <el-radio-button value="vowifi">VoWiFi</el-radio-button>
          <el-radio-button value="cellular" :disabled="isPCSC">蜂窝</el-radio-button>
        </el-radio-group>
      </el-form-item>

      <div v-if="form.task_type === 'sms'" class="form-grid">
        <el-form-item label="接收号码">
          <el-input v-model="form.payload.phone" maxlength="32" />
        </el-form-item>
        <el-form-item label="短信内容">
          <el-input v-model="form.payload.message" maxlength="1000" />
        </el-form-item>
      </div>
      <div v-if="form.task_type === 'call'" class="form-grid">
        <el-form-item label="呼叫号码">
          <el-input v-model="form.payload.phone" maxlength="32" />
        </el-form-item>
        <el-form-item label="保持时长（秒）">
          <el-input-number v-model="form.payload.hold_seconds" :min="1" :max="60" class="w-full" />
        </el-form-item>
      </div>

      <div class="form-grid form-grid-schedule">
        <el-form-item label="起始日期">
          <el-date-picker v-model="form.start_date" type="date" value-format="YYYY-MM-DD" class="w-full" />
        </el-form-item>
        <el-form-item label="执行时间">
          <el-time-picker v-model="form.run_time" format="HH:mm" value-format="HH:mm" class="w-full" />
        </el-form-item>
        <el-form-item label="间隔天数">
          <el-input-number v-model="form.interval_days" :min="1" :max="365" class="w-full" />
        </el-form-item>
        <el-form-item label="时区">
          <el-select v-model="form.timezone" filterable allow-create class="w-full">
            <el-option label="Asia/Shanghai" value="Asia/Shanghai" />
            <el-option label="Europe/London" value="Europe/London" />
            <el-option label="UTC" value="UTC" />
          </el-select>
        </el-form-item>
        <el-form-item label="失败重试">
          <el-input-number v-model="form.retry_count" :min="0" :max="10" class="w-full" />
        </el-form-item>
        <el-form-item label="完成通知">
          <el-switch v-model="form.notify" active-text="发送" inactive-text="关闭" />
        </el-form-item>
      </div>
    </el-form>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :loading="saving" @click="submit">保存</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.task-form :deep(.el-form-item) { margin-bottom: 18px; }
.form-grid { display: grid; grid-template-columns: minmax(0, 2fr) minmax(140px, 1fr); gap: 0 16px; }
.form-grid-schedule { grid-template-columns: repeat(3, minmax(0, 1fr)); }
@media (max-width: 640px) {
  .form-grid, .form-grid-schedule { grid-template-columns: minmax(0, 1fr); }
}
</style>
