<script setup lang="ts">
import { computed } from 'vue'
import type { DeviceOverviewItem } from '../types/api'
import StatusLight from './StatusLight.vue'
import { lifecycleStatusLabel, primaryLifecycleStatus } from '../utils/deviceLifecycle'
import { ArrowSync24Regular, Mail24Regular, Power24Regular, Sim24Regular } from '@vicons/fluent'

const props = defineProps<{
  device: DeviceOverviewItem
  rotating: boolean
  rebooting: boolean
  reconnecting: boolean
}>()

const emit = defineEmits<{
  rotateIp: []
  rebootModem: []
  reconnectVowifi: []
  openSms: []
}>()

const status = computed(() => primaryLifecycleStatus(props.device))

const operatorName = computed(() => {
  return props.device.modem?.operator || props.device.modem?.native_spn || '运营商不可用'
})

const identityItems = computed(() => [
  { label: 'IMEI', value: props.device.modem?.imei || '不可用' },
  { label: 'ICCID', value: props.device.modem?.iccid || '不可用' },
  { label: '协议', value: props.device.backend_mode?.toUpperCase() || '不可用' },
  { label: '接口', value: props.device.interface || '不可用' }
])
</script>

<template>
  <header class="device-workspace-header">
    <div class="device-identity">
      <div class="device-header-brand-icon" aria-hidden="true">
        <el-icon><Sim24Regular /></el-icon>
      </div>
      <div class="min-w-0">
        <span class="device-workspace-kicker">DEVICE WORKSPACE</span>
        <div class="device-workspace-title-row">
          <h1 class="device-workspace-title">{{ device.name || device.id }}</h1>
          <span class="device-workspace-status">
            <StatusLight :tone="status.tone" size="sm" :animated="status.animated" />
            {{ lifecycleStatusLabel(device.lifecycle_phase) || status.label }}
          </span>
        </div>
        <p class="device-operator"><span>运营商</span>{{ operatorName }}</p>
        <dl class="device-workspace-meta">
          <div v-for="item in identityItems" :key="item.label">
            <dt>{{ item.label }}</dt>
            <dd :title="item.value">{{ item.value }}</dd>
          </div>
        </dl>
      </div>
    </div>

    <div class="device-workspace-actions" aria-label="当前设备操作">
      <el-button @click="emit('openSms')" class="ui-glass-border !border-0">
        <el-icon><Mail24Regular /></el-icon>
        短信
      </el-button>
      <el-button v-if="device.vowifi_enabled" :loading="reconnecting" @click="emit('reconnectVowifi')" class="ui-glass-border !border-0">
        <el-icon><ArrowSync24Regular /></el-icon>
        重连 VoWiFi
      </el-button>
      <el-button v-else :loading="rotating" :disabled="!device.network_connected" @click="emit('rotateIp')" class="ui-glass-border !border-0">
        <el-icon><ArrowSync24Regular /></el-icon>
        切换 IP
      </el-button>
      <el-button :loading="rebooting" @click="emit('rebootModem')" class="ui-glass-border device-reboot-button !border-0">
        <el-icon><Power24Regular /></el-icon>
        重启模组
      </el-button>
    </div>
  </header>
</template>

<style scoped>
.device-header-brand-icon {
  width: 48px;
  height: 48px;
  border: 1px solid color-mix(in srgb, var(--ui-primary) 34%, var(--ui-border));
  border-radius: 12px;
  display: grid;
  place-items: center;
  flex-shrink: 0;
  background: color-mix(in srgb, var(--ui-primary) 11%, var(--ui-surface));
  color: var(--ui-primary);
  font-size: 23px;
}

.device-workspace-header {
  min-height: 132px;
  padding: 22px 24px 20px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  border-bottom: 1px solid var(--ui-border);
  background:
    linear-gradient(112deg, color-mix(in srgb, var(--ui-primary) 7%, transparent), transparent 38%);
}

.device-identity,
.device-workspace-actions,
.device-workspace-title-row,
.device-workspace-status {
  display: flex;
  align-items: center;
}

.device-identity {
  min-width: 0;
  gap: 14px;
}

.device-workspace-kicker {
  color: var(--ui-primary);
  font: 700 var(--ui-font-caption) "v-mono", ui-monospace, monospace;
  letter-spacing: .16em;
}

.device-workspace-title-row {
  min-width: 0;
  margin-top: 3px;
  gap: 10px;
}

.device-workspace-title {
  min-width: 0;
  margin: 0;
  overflow: hidden;
  color: var(--ui-text);
  font-size: 22px;
  font-weight: 650;
  letter-spacing: -.02em;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.device-workspace-status {
  flex: 0 0 auto;
  gap: 5px;
  color: var(--ui-text-muted);
  font-size: var(--ui-font-caption);
}

.device-operator {
  margin: 3px 0 0;
  color: var(--ui-text-muted);
  font-size: var(--ui-font-body-sm);
}

.device-operator span {
  margin-right: 10px;
}

.device-workspace-meta {
  margin: 11px 0 0;
  display: grid;
  grid-template-columns: repeat(4, minmax(74px, auto));
  gap: 8px 18px;
}

.device-workspace-meta div {
  min-width: 0;
}

.device-workspace-meta dt {
  color: var(--ui-text-muted);
  font: 700 var(--ui-font-caption) "v-mono", ui-monospace, monospace;
  letter-spacing: .1em;
}

.device-workspace-meta dd {
  max-width: 180px;
  margin: 2px 0 0;
  overflow: hidden;
  color: var(--ui-text);
  font: var(--ui-font-body-sm) "v-mono", ui-monospace, monospace;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.device-workspace-actions {
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
}

.device-workspace-actions :deep(.el-button + .el-button) {
  margin-left: 0;
}

.device-reboot-button:hover,
.device-reboot-button:focus-visible {
  color: var(--ui-danger);
}

@media (max-width: 920px) {
  .device-workspace-header {
    align-items: flex-start;
    flex-direction: column;
  }

  .device-workspace-actions {
    width: 100%;
    justify-content: flex-start;
  }
}

@media (max-width: 600px) {
  .device-workspace-header {
    padding: 18px 16px;
  }

  .device-identity {
    align-items: flex-start;
  }

  .device-workspace-title-row {
    align-items: flex-start;
    flex-direction: column;
    gap: 3px;
  }

  .device-workspace-title {
    font-size: 19px;
  }

  .device-workspace-meta {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .device-workspace-actions :deep(.el-button) {
    flex: 1 1 calc(50% - 4px);
  }
}
</style>
