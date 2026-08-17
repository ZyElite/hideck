<script setup lang="ts">
import { computed, watch } from 'vue'
import type { DeviceConfigDTO, DiscoveredDevice } from '../types/api'
import { isWwanQmiControlPath } from '../utils/deviceBackend'
import { ArrowSync24Regular, Save24Regular } from '@vicons/fluent'

const props = defineProps<{
  modelValue: boolean
  discovering: boolean
  unconfiguredDiscovered: DiscoveredDevice[]
  addSelected: DiscoveredDevice | null
  addConfig: DeviceConfigDTO
  addSaving: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  'select-device': [device: DiscoveredDevice]
  save: []
}>()

function closeDialog() {
  emit('update:modelValue', false)
}

function handleDialogModelUpdate(value: boolean) {
  emit('update:modelValue', value)
}

function discoveryIdentity(d: DiscoveredDevice | null | undefined): string {
  if (!d) return ''
  return String(d.discovery_key || `${d.usb_path || ''}|${d.at_port || ''}`)
}

function isQmiDiscovery(d: DiscoveredDevice | null | undefined): boolean {
  if (!d) return false
  return String(d.mode || '').toLowerCase() === 'qmi'
}

function discoveryModeText(d: DiscoveredDevice | null | undefined): string {
  const mode = String(d?.mode || 'unknown').toLowerCase()
  if (mode === 'qmi') return 'QMI'
  if (mode === 'mbim') return 'MBIM'
  if (mode === 'ecm') return 'ECM'
  if (mode === 'rndis') return 'RNDIS'
  if (mode === 'ncm') return 'NCM'
  if (mode === 'pcsc') return 'PC/SC'
  return 'UNKNOWN'
}

const isQMIBackendOnly = computed(() => {
  const control = props.addSelected?.control_path || props.addConfig?.control_device
  return isWwanQmiControlPath(control) || (isQmiDiscovery(props.addSelected) && Boolean(String(control || '').trim()))
})
const isMBIMBackendOnly = computed(
  () => String(props.addSelected?.mode || '').toLowerCase() === 'mbim'
)
const isPCSCBackendOnly = computed(
  () => String(props.addSelected?.hardware_kind || props.addSelected?.mode || '').toLowerCase() === 'pcsc'
)

watch(
  isQMIBackendOnly,
  (locked) => {
    if (locked && props.addConfig) {
      props.addConfig.device_backend = 'qmi'
    }
  },
  { immediate: true }
)

watch(
  isPCSCBackendOnly,
  (locked) => {
    if (locked && props.addConfig) {
      props.addConfig.device_backend = 'pcsc'
      props.addConfig.esim_transport = 'pcsc'
    }
  },
  { immediate: true }
)

watch(
  isMBIMBackendOnly,
  (locked) => {
    if (locked && props.addConfig) {
      props.addConfig.device_backend = 'mbim'
    }
  },
  { immediate: true }
)
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    @update:model-value="handleDialogModelUpdate"
    title="添加设备配置"
    width="min(720px, 92vw)"
    class="glass-modal add-device-dialog"
  >
    <p class="add-device-hint">选择一个未配置设备，系统会填入 ID、端口和识别信息。</p>
    <div class="add-device-list">
      <div v-if="discovering" class="add-device-empty">
        <el-icon class="is-loading" size="28"><ArrowSync24Regular /></el-icon>
        <span>正在探测设备...</span>
      </div>
      <template v-else>
        <button
          v-for="d in unconfiguredDiscovered"
          :key="discoveryIdentity(d)"
          type="button"
          class="add-device-option"
          :class="{
            'is-selected': discoveryIdentity(addSelected) === discoveryIdentity(d),
            'is-degraded': d.degraded
          }"
          :aria-disabled="!!d.degraded"
          @click="emit('select-device', d)"
        >
          <div class="add-device-option-title">
            <strong>{{ d.reader_name || d.net_interface || '--' }} · {{ d.driver_name || '--' }}</strong>
            <el-tag size="small" :type="isQmiDiscovery(d) ? 'success' : 'warning'">{{ discoveryModeText(d) }}</el-tag>
          </div>
          <div class="add-device-option-meta">
            <template v-if="d.hardware_kind === 'pcsc'">
              卡片: {{ d.card_present ? '已插入' : '未插入' }} · ATR: {{ d.atr || '--' }} · USB: {{ d.usb_path || '--' }}
            </template>
            <template v-else>
              {{ d.control_path }} · AT: {{ d.at_port || '--' }} · IMEI: {{ d.imei || '--' }} · USB: {{ d.usb_path || '--' }}
            </template>
          </div>
          <div v-if="d.degraded" class="add-device-option-warn">
            {{ d.hardware_kind === 'pcsc' ? '读卡器内没有可用卡片，暂不可添加。' : '无法读取 IMEI（控制口可能挂死），请稍后重试。' }}
          </div>
        </button>
        <div v-if="unconfiguredDiscovered.length === 0" class="add-device-empty">
          暂无可添加设备
        </div>
      </template>
    </div>

    <div class="add-device-grid">
      <label class="add-device-field">
        <span>ID</span>
        <el-input v-model="addConfig.id" placeholder="例如 wwan0" />
      </label>
      <label v-if="isPCSCBackendOnly" class="add-device-field">
        <span>SIM PIN 环境变量名</span>
        <el-input v-model="addConfig.sim_pin_env" placeholder="例如 HIDECK_SIM_PIN_READER1" />
      </label>
      <label class="add-device-field">
        <span>名称</span>
        <el-input v-model="addConfig.name" placeholder="显示名称（可选）" />
      </label>
      <label v-if="!isPCSCBackendOnly" class="add-device-field">
        <span>IMEI 绑定</span>
        <el-input v-model="addConfig.modem_imei" disabled placeholder="选中设备后自动填入" />
      </label>
      <label class="add-device-field">
        <span>USB 路径</span>
        <el-input v-model="addConfig.usb_path" disabled />
      </label>
      <label v-if="!isPCSCBackendOnly" class="add-device-field">
        <span>网卡接口</span>
        <el-input v-model="addConfig.interface" disabled />
      </label>
      <label v-if="!isPCSCBackendOnly" class="add-device-field">
        <span>AT 端口</span>
        <el-input v-model="addConfig.at_port" disabled />
      </label>
      <label class="add-device-field">
        <span>控制设备</span>
        <el-input v-model="addConfig.control_device" disabled />
      </label>
      <div class="add-device-field add-device-backend">
        <span>设备后端模式</span>
        <div class="add-device-backend-row">
          <small>
            {{ isPCSCBackendOnly ? '固定 PC/SC，无蜂窝射频'
               : (isQMIBackendOnly ? '固定 QMI，AT 口仅用于终端'
               : (isMBIMBackendOnly ? '固定 MBIM，AT 口仅用于终端'
               : 'AT=串口 / QMI=纯 QMI')) }}
          </small>
          <el-select
            v-model="addConfig.device_backend"
            style="width: 110px"
            placeholder="AT"
            :disabled="isQMIBackendOnly || isMBIMBackendOnly || isPCSCBackendOnly"
          >
            <el-option v-if="!isMBIMBackendOnly && !isPCSCBackendOnly" label="AT" value="at" :disabled="isQMIBackendOnly" />
            <el-option v-if="!isMBIMBackendOnly && !isPCSCBackendOnly" label="QMI" value="qmi" :disabled="!addConfig.control_device" />
            <el-option v-if="isMBIMBackendOnly" label="MBIM" value="mbim" />
            <el-option v-if="isPCSCBackendOnly" label="PC/SC" value="pcsc" />
          </el-select>
        </div>
      </div>
    </div>

    <template #footer>
      <div class="add-device-footer">
        <el-button @click="closeDialog" class="ui-button-plain">取消</el-button>
        <el-button type="primary" :loading="addSaving" @click="emit('save')" class="!border-0">
          <el-icon><Save24Regular /></el-icon>
          保存
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<style scoped>
.add-device-hint {
  margin: 0 0 12px;
  color: var(--ui-text-muted);
  font-size: var(--ui-font-body-sm);
}

.add-device-list {
  max-height: 220px;
  padding-right: 2px;
  display: grid;
  gap: 8px;
  overflow: auto;
}

.add-device-empty {
  min-height: 88px;
  display: grid;
  place-content: center;
  gap: 8px;
  color: var(--ui-text-muted);
  font-size: var(--ui-font-body-sm);
  text-align: center;
}

.add-device-option {
  width: 100%;
  padding: 12px 14px;
  border: 1px solid var(--ui-border);
  border-radius: var(--ui-radius-md);
  background: var(--ui-surface-muted);
  color: var(--ui-text);
  text-align: left;
  cursor: pointer;
}

.add-device-option:hover {
  border-color: color-mix(in srgb, var(--ui-primary) 38%, var(--ui-border));
}

.add-device-option.is-selected {
  border-color: color-mix(in srgb, var(--ui-primary) 58%, var(--ui-border));
  background: color-mix(in srgb, var(--ui-primary) 12%, var(--ui-surface));
}

.add-device-option.is-degraded {
  opacity: .78;
  cursor: not-allowed;
}

.add-device-option-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.add-device-option-title strong {
  min-width: 0;
  font-size: var(--ui-font-body);
  font-weight: 650;
}

.add-device-option-meta,
.add-device-option-warn {
  margin-top: 4px;
  color: var(--ui-text-muted);
  font-size: var(--ui-font-caption);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.add-device-option-warn {
  color: var(--ui-warning);
  white-space: normal;
}

.add-device-grid {
  margin-top: 16px;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px 16px;
}

.add-device-field {
  display: grid;
  gap: 6px;
}

.add-device-field > span {
  color: var(--ui-text-muted);
  font-size: var(--ui-font-caption);
  font-weight: 700;
  letter-spacing: .04em;
}

.add-device-backend-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.add-device-backend-row small {
  color: var(--ui-text-muted);
  font-size: var(--ui-font-caption);
  line-height: 1.4;
}

.add-device-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

@media (max-width: 639px) {
  .add-device-grid,
  .add-device-backend-row {
    grid-template-columns: 1fr;
    display: grid;
  }
}
</style>
