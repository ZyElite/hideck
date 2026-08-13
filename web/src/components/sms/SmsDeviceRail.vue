<script setup lang="ts">
import { DeviceEq24Regular, Grid24Regular } from '@vicons/fluent'
import type { SmsDeviceChannelPresentation } from '../../utils/smsPresentation'

defineProps<{
  items: readonly SmsDeviceChannelPresentation[]
  selectedId: string
}>()

const emit = defineEmits<{
  select: [deviceId: string]
}>()
</script>

<template>
  <aside class="sms-device-rail" aria-label="短信设备通道">
    <header>
      <span>设备</span>
      <strong>{{ items[0]?.statusLabel || '0/0 在线' }}</strong>
    </header>
    <div class="sms-device-scroll">
      <button
        v-for="item in items"
        :key="item.id"
        type="button"
        class="sms-device-switch"
        :class="{ 'is-active': selectedId === item.id, 'is-all': item.id === 'all' }"
        :aria-label="item.accessibilityLabel"
        :aria-pressed="selectedId === item.id"
        :title="`${item.label} · ${item.detail}`"
        @click="emit('select', item.id)"
      >
        <el-icon class="sms-device-icon">
          <Grid24Regular v-if="item.id === 'all'" />
          <DeviceEq24Regular v-else />
        </el-icon>
        <span class="sms-device-copy">
          <strong>{{ item.label }}</strong>
          <small>{{ item.detail }}</small>
        </span>
        <i
          v-if="item.id !== 'all'"
          class="sms-device-presence"
          :class="{ 'is-online': item.online }"
          aria-hidden="true"
        />
      </button>
    </div>
  </aside>
</template>

<style scoped>
.sms-device-rail {
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--ui-border);
  background: var(--ui-surface);
}

.sms-device-rail > header {
  min-height: 54px;
  padding: 0 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  color: var(--ui-text-muted);
  font-size: 12px;
}

.sms-device-rail > header strong {
  color: var(--ui-primary);
  font-size: var(--ui-font-body-sm);
  font-weight: 600;
}

.sms-device-scroll {
  min-height: 0;
  padding: 4px 12px 12px;
  overflow: auto;
  scrollbar-width: thin;
}

.sms-device-switch {
  position: relative;
  width: 100%;
  min-height: 62px;
  padding: 9px 10px;
  display: grid;
  grid-template-columns: 22px minmax(0, 1fr) 8px;
  align-items: center;
  gap: 10px;
  border: 1px solid transparent;
  border-bottom-color: var(--ui-border-muted);
  border-radius: 9px;
  background: transparent;
  color: var(--ui-text);
  cursor: pointer;
  text-align: left;
  transition: color 140ms var(--ui-ease-out), background-color 140ms var(--ui-ease-out), border-color 140ms var(--ui-ease-out), transform 120ms var(--ui-ease-out);
}

.sms-device-switch.is-all { margin-bottom: 5px; }
.sms-device-switch:active { transform: scale(.98); }

.sms-device-switch.is-active {
  border-color: color-mix(in srgb, var(--ui-primary) 46%, var(--ui-border));
  background: color-mix(in srgb, var(--ui-primary) 11%, var(--ui-surface));
  color: var(--ui-primary);
}

.sms-device-icon { font-size: 19px; }
.sms-device-copy { min-width: 0; }

.sms-device-copy strong,
.sms-device-copy small {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sms-device-copy strong { font-size: 13px; }
.sms-device-copy small { margin-top: 2px; color: var(--ui-text-muted); font-size: var(--ui-font-caption); }

.sms-device-presence {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--ui-text-muted);
}

.sms-device-presence.is-online {
  background: var(--ui-success);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--ui-success) 13%, transparent);
}

@media (hover: hover) and (pointer: fine) {
  .sms-device-switch:hover:not(.is-active) { background: var(--ui-surface-muted); }
}

@media (max-width: 1180px) {
  .sms-device-rail > header { display: none; }
  .sms-device-scroll { padding: 7px; }
  .sms-device-switch {
    min-height: 54px;
    padding: 0;
    grid-template-columns: 1fr;
    place-items: center;
  }
  .sms-device-copy { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
  .sms-device-presence { position: absolute; top: 8px; right: 8px; width: 6px; height: 6px; }
}

@media (max-width: 820px) {
  .sms-device-scroll { padding: 5px; }
  .sms-device-switch { min-height: 48px; }
  .sms-device-icon { font-size: 18px; }
}

@media (prefers-reduced-motion: reduce) {
  .sms-device-switch { transition-property: color, background-color, border-color; }
  .sms-device-switch:active { transform: none; }
}
</style>
