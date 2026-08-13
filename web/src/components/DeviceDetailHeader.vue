<script setup lang="ts">
import type { DeviceOverviewItem } from '../types/api'
import { ArrowSync24Regular, Power24Regular, Mail24Regular } from '@vicons/fluent'

defineProps<{
  device: DeviceOverviewItem
  rotating: boolean
  rebooting: boolean
  reconnectingVoWiFi: boolean
}>()

const emit = defineEmits<{
  'copy-text': [value: string]
  'rotate-ip': []
  'reboot-modem': []
  'reconnect-vowifi': []
  'open-sms': []
}>()
</script>

<template>
  <header class="device-workspace-header ui-card">
    <div class="device-identity">
      <div class="device-header-brand-icon">V</div>
      <div class="min-w-0">
        <span class="device-workspace-kicker">DEVICE WORKSPACE</span>
        <div class="device-workspace-title">{{ device.name || device.id }}</div>
        <div class="device-workspace-meta">
          <button type="button" @click="emit('copy-text', device.id)">{{ device.id }}</button>
          <span>·</span>
          <button type="button" @click="emit('copy-text', device.public_ip || '')">{{ device.public_ip || '无公网 IP' }}</button>
        </div>
      </div>
    </div>

    <div class="device-workspace-actions">
      <div class="device-action-primary">
        <el-button @click="emit('open-sms')" class="ui-glass-border !border-0">
          <el-icon><Mail24Regular /></el-icon>
          短信
        </el-button>
      </div>
      <div class="device-action-system">
        <el-button v-if="device?.vowifi_enabled" :loading="reconnectingVoWiFi" @click="emit('reconnect-vowifi')" class="ui-glass-border !border-0">
          <el-icon><ArrowSync24Regular /></el-icon>
          重连 VoWiFi
        </el-button>
        <el-button v-else :loading="rotating" :disabled="!device?.network_connected" @click="emit('rotate-ip')" class="ui-glass-border !border-0">
          <el-icon><ArrowSync24Regular /></el-icon>
          切换 IP
        </el-button>
        <el-button :loading="rebooting" @click="emit('reboot-modem')" class="ui-glass-border !border-0 hover:!text-red-600">
          <el-icon><Power24Regular /></el-icon>
          重启模组
        </el-button>
      </div>
    </div>
  </header>
</template>

<style scoped>
.device-header-brand-icon {
  width: 2.75rem;
  height: 2.75rem;
  border-radius: 0.75rem;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  background: linear-gradient(135deg, #06b6d4, #14b8a6);
  color: #fff;
  font-family: "v-sans", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  font-size: 1.15rem;
  font-weight: 700;
  box-shadow: 0 10px 22px rgba(6, 182, 212, 0.2);
}

.device-workspace-header {
  min-height: 116px;
  padding: 22px 24px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  background:
    linear-gradient(110deg, color-mix(in srgb, var(--ui-primary) 8%, transparent), transparent 32%),
    var(--ui-surface);
}

.device-identity,
.device-workspace-actions,
.device-action-system {
  display: flex;
  align-items: center;
}

.device-identity {
  min-width: 0;
  gap: 14px;
}

.device-workspace-kicker {
  color: var(--ui-primary);
  font: 700 9px "v-mono", monospace;
  letter-spacing: .14em;
}

.device-workspace-title {
  margin-top: 4px;
  overflow: hidden;
  color: var(--ui-text);
  font-size: 24px;
  font-weight: 650;
  letter-spacing: -.02em;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.device-workspace-meta {
  margin-top: 5px;
  display: flex;
  gap: 7px;
  color: var(--ui-text-muted);
  font: 11px "v-mono", monospace;
}

.device-workspace-meta button {
  overflow: hidden;
  border: 0;
  background: transparent;
  color: inherit;
  text-overflow: ellipsis;
  cursor: copy;
  white-space: nowrap;
}

.device-workspace-actions {
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 12px;
}

.device-action-system {
  gap: 8px;
  padding-left: 12px;
  border-left: 1px solid var(--ui-border);
}

@media (max-width: 760px) {
  .device-workspace-header,
  .device-workspace-actions {
    align-items: stretch;
    flex-direction: column;
  }

  .device-workspace-actions {
    width: 100%;
  }

  .device-action-system {
    padding: 10px 0 0;
    flex-wrap: wrap;
    border-top: 1px solid var(--ui-border);
    border-left: 0;
  }
}

</style>
