<script setup lang="ts">
import { computed } from 'vue'
import {
  Router24Regular,
  Delete24Regular,
  Save24Regular
} from '@vicons/fluent'
import type { DeviceConfigDTO, DeviceOverviewItem } from '../types/api'
import { normalizeManagedDeviceBackend } from '../utils/deviceBackend'

const props = defineProps<{
  editConfig: DeviceConfigDTO | null
  deviceStatus?: DeviceOverviewItem | null
  saving: boolean
  deleting: boolean
}>()

const emit = defineEmits<{
  save: []
  delete: []
}>()

const activeControlDevice = computed(() => props.deviceStatus?.control_device || props.editConfig?.control_device)
const activeInterface = computed(() => props.deviceStatus?.interface || props.editConfig?.interface)
const activeATPort = computed(() => props.deviceStatus?.at_port || props.editConfig?.at_port)
const activeUsbPath = computed(() => props.deviceStatus?.usb_path || props.editConfig?.usb_path)

const configuredBackend = computed(() => String(props.editConfig?.device_backend || '').toLowerCase())
const isManagedBackend = computed(
  () => normalizeManagedDeviceBackend(configuredBackend.value) !== null
)
const isPCSCBackend = computed(() => configuredBackend.value === 'pcsc')
</script>

<template>
  <section class="config-workspace">
    <header class="config-workspace-header">
      <div class="config-heading-icon" aria-hidden="true">
        <el-icon><Router24Regular /></el-icon>
      </div>
      <div>
        <span>DEVICE CONFIGURATION</span>
        <h2>设备配置</h2>
        <p>编辑数据库中的设备绑定；自动探测字段保持只读</p>
      </div>
    </header>

    <div v-if="editConfig" class="config-form-surface">
      <div class="config-columns">
        <section>
          <h3>基本信息</h3>
          <div class="config-field">
            <label>设备名称</label>
            <el-input v-model="editConfig.name" placeholder="显示名称" />
          </div>
          <div class="config-field">
            <label>ID</label>
            <el-input v-model="editConfig.id" disabled />
          </div>
          <div class="config-field">
            <label>{{ isPCSCBackend ? '读卡器名称' : 'IMEI 绑定' }}</label>
            <el-input :model-value="isPCSCBackend ? editConfig.pcsc_reader_name : editConfig.modem_imei" disabled placeholder="自动识别（添加时绑定）" />
          </div>

          <h3>自动探测端口（只读）</h3>
          <div class="config-field">
            <label>控制设备</label>
            <el-input :model-value="activeControlDevice || ''" disabled placeholder="由系统自动探测" />
          </div>
          <div v-if="!isPCSCBackend" class="config-field">
            <label>AT 端口</label>
            <el-input :model-value="activeATPort || ''" disabled placeholder="由系统自动探测" />
          </div>
        </section>

        <section>
          <h3>连接协议</h3>
          <div class="config-protocol-field">
          <div>
              <strong>设备运行模式</strong>
              <small>协议切换保存后将触发设备重新绑定</small>
          </div>
          <el-radio-group
            v-if="isManagedBackend"
            v-model="editConfig.device_backend"
            size="small"
            :disabled="saving"
            aria-label="设备运行模式"
          >
            <el-radio-button value="qmi">QMI</el-radio-button>
            <el-radio-button value="mbim">MBIM</el-radio-button>
          </el-radio-group>
          <el-select v-else v-model="editConfig.device_backend" style="width: 120px" disabled>
            <el-option v-if="isPCSCBackend" label="PC/SC" value="pcsc" />
            <el-option label="AT" value="at" />
          </el-select>
        </div>

          <div v-if="!isPCSCBackend" class="config-field">
            <label>网络接口</label>
            <el-input :model-value="activeInterface || ''" disabled placeholder="由系统自动探测" />
          </div>
          <div class="config-field">
            <label>USB 路径</label>
            <el-input :model-value="activeUsbPath || ''" disabled placeholder="由系统自动探测" />
          </div>
          <div v-if="isPCSCBackend" class="config-field">
            <label>SIM PIN 环境变量名</label>
            <el-input v-model="editConfig.sim_pin_env" placeholder="例如 VOHIVE_SIM_PIN_READER1" />
          </div>
        </section>
      </div>

      <p class="config-notice">协议或绑定配置变更可能导致设备短暂离线；保存仍使用现有设备配置接口。</p>
      <footer class="config-actions">
        <el-button type="danger" :loading="deleting" @click="emit('delete')" class="!border-0">
          <el-icon><Delete24Regular /></el-icon>
          删除设备
        </el-button>
        <el-button type="primary" :loading="saving" @click="emit('save')" class="!border-0">
          <el-icon><Save24Regular /></el-icon>
          保存配置
        </el-button>
      </footer>
    </div>
  </section>
</template>

<style scoped>
.config-workspace-header { min-height: 82px; display: flex; align-items: center; gap: 12px; }
.config-heading-icon { width: 44px; height: 44px; display: grid; place-items: center; border: 1px solid var(--ui-border); border-radius: 11px; color: var(--ui-primary); font-size: 22px; }
.config-workspace-header span { color: var(--ui-primary); font: 700 var(--ui-font-caption) "v-mono", ui-monospace, monospace; letter-spacing: .15em; }
.config-workspace-header h2 { margin: 3px 0 0; color: var(--ui-text); font-size: 20px; font-weight: 650; }
.config-workspace-header p { margin: 3px 0 0; color: var(--ui-text-muted); font-size: var(--ui-font-body-sm); }

.config-form-surface { overflow: hidden; border: 1px solid var(--ui-border); border-radius: 12px; background: var(--ui-surface-strong); }
.config-columns { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
.config-columns > section { min-width: 0; padding: 22px; }
.config-columns > section + section { border-left: 1px solid var(--ui-border); }
.config-columns h3 { margin: 0 0 14px; color: var(--ui-text); font-size: var(--ui-font-body); font-weight: 650; }
.config-columns h3:not(:first-child) { margin-top: 26px; padding-top: 18px; border-top: 1px solid var(--ui-border-muted); }
.config-field { margin-top: 13px; }
.config-field label { display: block; margin-bottom: 5px; color: var(--ui-text-muted); font: 700 var(--ui-font-caption) "v-mono", ui-monospace, monospace; letter-spacing: .08em; }
.config-protocol-field { min-height: 62px; padding: 11px 12px; display: flex; align-items: center; justify-content: space-between; gap: 12px; border: 1px solid var(--ui-border); border-radius: 8px; background: var(--ui-surface-muted); }
.config-protocol-field strong { display: block; color: var(--ui-text); font-size: var(--ui-font-body-sm); }
.config-protocol-field small { display: block; margin-top: 2px; color: var(--ui-text-muted); font-size: var(--ui-font-caption); }
.config-notice { margin: 0; padding: 11px 22px; border-top: 1px solid var(--ui-border); border-bottom: 1px solid var(--ui-border); background: color-mix(in srgb, var(--ui-warning) 6%, var(--ui-surface)); color: var(--ui-text-muted); font-size: var(--ui-font-body-sm); }
.config-actions { min-height: 70px; padding: 14px 18px; display: flex; align-items: center; justify-content: space-between; gap: 10px; }

@media (max-width: 760px) {
  .config-columns { grid-template-columns: minmax(0, 1fr); }
  .config-columns > section { padding: 18px 16px; }
  .config-columns > section + section { border-top: 1px solid var(--ui-border); border-left: 0; }
}

@media (max-width: 520px) {
  .config-workspace-header { align-items: flex-start; }
  .config-protocol-field { align-items: stretch; flex-direction: column; }
  .config-actions { align-items: stretch; flex-direction: column-reverse; }
  .config-actions :deep(.el-button) { width: 100%; min-height: 44px; margin-left: 0; }
}
</style>
