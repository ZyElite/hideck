<script setup lang="ts">
import { computed } from 'vue'
import {
  Checkmark12Regular,
  Dismiss12Regular,
  Open20Regular,
  Subtract12Regular
} from '@vicons/fluent'
import type { DashboardDevice } from '../types/api'
import {
  canAnimateDashboardConnection,
  createDashboardDevicePresentation
} from '../utils/dashboardPresentation'

const props = defineProps<{
  device?: DashboardDevice
}>()

const emit = defineEmits<{
  (event: 'open', deviceID?: string): void
}>()

const emptyStages = Object.freeze(['SIM', 'Access', 'Tunnel', 'IMS', 'SMS'].map(key => ({ key, ready: undefined })))
const presentation = computed(() => props.device ? createDashboardDevicePresentation(props.device) : null)
const stages = computed(() => presentation.value?.stages || emptyStages)
const pathIsFlowing = computed(() => !!props.device && canAnimateDashboardConnection(props.device))
const focusMeta = computed(() => {
  if (!props.device || !presentation.value) return '设备管理 · 等待接入'
  return [presentation.value.operator, props.device.id, presentation.value.connectionType].join(' · ')
})
const focusDetail = computed(() => {
  if (!props.device) return '请先在设备管理中添加或接管设备'
  if (!props.device.healthy) return '设备离线，等待链路恢复'
  if (props.device.vowifi_active) return '通过 Wi-Fi 注册到 IMS'
  return '设备在线，VoWiFi 尚未激活'
})
const primaryFact = computed(() => {
  if (props.device?.vowifi_active) {
    return {
      label: '链路状态',
      value: presentation.value?.connectionState || '等待状态',
      hint: 'Wi-Fi Calling'
    }
  }
  return {
    label: '蜂窝信号',
    value: presentation.value?.signal || '不可用',
    hint: props.device ? '当前设备' : '暂无设备'
  }
})

function stageStatusLabel(ready: boolean | undefined): string {
  if (ready === true) return '已就绪'
  if (ready === false) return '失败'
  return '等待状态'
}
</script>

<template>
  <section class="connection-stage" aria-label="当前设备连接焦点">
    <div class="connection-stage-main">
      <header class="connection-stage-heading">
        <span class="dashboard-eyebrow">ACTIVE DEVICE</span>
        <span
          class="focus-device-status"
          :class="device ? device.healthy ? 'is-online' : 'is-offline' : 'is-idle'"
        >
          <i aria-hidden="true" />
          {{ presentation?.statusLabel || '等待设备' }}
        </span>
      </header>

      <h2>{{ presentation?.connectionTitle || '等待设备接入' }}</h2>
      <strong :class="{ 'is-idle': !device }">{{ presentation?.connectionState || '暂无可用设备' }}</strong>
      <p>{{ focusMeta }}</p>
      <small class="focus-detail">{{ focusDetail }}</small>

      <div
        class="connection-path"
        :class="{ 'is-flowing': pathIsFlowing }"
        aria-label="VoWiFi 服务链路"
      >
        <div class="connection-path-track" aria-hidden="true">
          <span class="connection-signal" />
        </div>
        <div
          v-for="stage in stages"
          :key="stage.key"
          class="connection-path-step"
          :class="{
            'is-ready': stage.ready === true,
            'is-failed': stage.ready === false
          }"
          :aria-label="`${stage.key}：${stageStatusLabel(stage.ready)}`"
        >
          <span aria-hidden="true">
            <Checkmark12Regular v-if="stage.ready === true" />
            <Dismiss12Regular v-else-if="stage.ready === false" />
            <Subtract12Regular v-else />
          </span>
          <small>{{ stage.key }}</small>
        </div>
      </div>
    </div>

    <div class="connection-signal-field" aria-hidden="true">
      <i v-for="line in 5" :key="line" />
    </div>

    <aside class="connection-stage-aside" aria-label="当前设备网络事实">
      <dl>
        <div class="focus-fact-primary">
          <dt>{{ primaryFact.label }}</dt>
          <dd :class="{ 'is-tabular': !device?.vowifi_active }">{{ primaryFact.value }}</dd>
          <small>{{ primaryFact.hint }}</small>
        </div>
        <div>
          <dt>运营商 / 连接</dt>
          <dd>{{ presentation ? `${presentation.operator} · ${presentation.connectionType}` : '不可用' }}</dd>
        </div>
        <template v-if="device?.vowifi_active">
          <div>
            <dt>接入方式</dt>
            <dd>Wi-Fi</dd>
          </div>
          <div>
            <dt>IMS / SMS</dt>
            <dd>{{ presentation?.connectionState || '等待状态' }}</dd>
          </div>
        </template>
        <template v-else>
          <div>
            <dt>公网 IPv4</dt>
            <dd class="is-address" :title="presentation?.ipv4 || '未分配'">{{ presentation?.ipv4 || '未分配' }}</dd>
          </div>
          <div>
            <dt>公网 IPv6</dt>
            <dd class="is-address" :title="presentation?.ipv6 || '未分配'">{{ presentation?.ipv6 || '未分配' }}</dd>
          </div>
        </template>
      </dl>
      <button type="button" class="focus-open-button" @click="emit('open', device?.id)">
        <span>{{ device ? '打开设备工作区' : '前往设备管理' }}</span>
        <Open20Regular aria-hidden="true" />
      </button>
    </aside>
  </section>
</template>

<style scoped>
.connection-stage {
  position: relative;
  min-height: 390px;
  margin-bottom: 18px;
  padding: clamp(28px, 4vw, 52px);
  display: grid;
  grid-template-columns: minmax(510px, 1.1fr) minmax(260px, .8fr) 290px;
  gap: 0;
  overflow: hidden;
  border: 1px solid var(--ui-border);
  border-radius: 22px;
  background:
    radial-gradient(circle at 56% 51%, color-mix(in srgb, var(--ui-primary) 13%, transparent), transparent 31%),
    linear-gradient(132deg, var(--ui-surface) 0 48%, color-mix(in srgb, var(--ui-surface) 90%, #06120e) 100%);
  box-shadow: var(--ui-shadow-sm);
}

.connection-stage-main,
.connection-stage-aside { position: relative; z-index: 1; min-width: 0; }
.connection-stage-heading { display: flex; align-items: center; gap: 12px; }
.dashboard-eyebrow { color: var(--ui-primary); font: 700 10px "v-mono", monospace; letter-spacing: .14em; }
.focus-device-status { display: inline-flex; align-items: center; gap: 7px; color: var(--ui-text-muted); font-size: 11px; }
.focus-device-status i { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
.focus-device-status.is-online { color: var(--ui-success); }
.focus-device-status.is-offline { color: var(--ui-danger); }
.focus-device-status.is-idle { color: var(--ui-text-muted); }
.focus-device-status.is-online i { animation: online-pulse 2.4s var(--ui-ease-in-out) infinite; }
.connection-stage h2 { margin: 18px 0 -4px; color: var(--ui-text); font-size: clamp(54px, 5vw, 80px); font-weight: 560; letter-spacing: -.045em; line-height: .98; }
.connection-stage-main > strong { display: block; margin: 16px 0; color: var(--ui-primary); font-size: 42px; font-weight: 500; line-height: 1.1; }
.connection-stage-main > strong.is-idle { color: var(--ui-warning); }
.connection-stage-main > p { margin: 0; color: color-mix(in srgb, var(--ui-text) 78%, transparent); font-size: 19px; overflow-wrap: anywhere; }
.focus-detail { display: block; margin-top: 22px; color: var(--ui-text-muted); font-size: 15px; }

.connection-path {
  position: relative;
  max-width: 650px;
  margin-top: 56px;
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  container-type: inline-size;
}
.connection-path-track { position: absolute; top: 18px; right: 9%; left: 9%; height: 1px; background: var(--ui-border); }
.connection-path.is-flowing .connection-path-track { background: linear-gradient(90deg, color-mix(in srgb, var(--ui-primary) 34%, var(--ui-border)), var(--ui-primary), color-mix(in srgb, var(--ui-primary) 34%, var(--ui-border))); }
.connection-signal { position: absolute; top: -3px; left: 0; width: 7px; height: 7px; border-radius: 50%; opacity: 0; background: var(--ui-primary); box-shadow: 0 0 14px var(--ui-primary); }
.connection-path.is-flowing .connection-signal { animation: connection-signal 2.4s linear infinite; }
.connection-path-step { position: relative; z-index: 1; min-width: 0; display: grid; place-items: center; gap: 9px; color: var(--ui-text-muted); }
.connection-path-step > span { width: 40px; height: 40px; display: grid; place-items: center; border: 1px solid var(--ui-border); border-radius: 50%; background: var(--ui-surface); color: inherit; }
.connection-path-step svg { width: 15px; height: 15px; }
.connection-path-step small { font-size: 11px; }
.connection-path-step.is-ready { color: var(--ui-primary); }
.connection-path-step.is-ready > span { border-color: var(--ui-primary); box-shadow: 0 0 22px color-mix(in srgb, var(--ui-primary) 18%, transparent); }
.connection-path-step.is-failed { color: var(--ui-danger); }
.connection-path-step.is-failed > span { border-color: var(--ui-danger); }

.connection-signal-field { position: relative; align-self: center; height: 250px; opacity: .48; }
.connection-signal-field::before { position: absolute; inset: 0; background-image: radial-gradient(circle, color-mix(in srgb, var(--ui-primary) 42%, transparent) 1px, transparent 1.4px); background-size: 16px 16px; mask-image: radial-gradient(ellipse, #000, transparent 68%); content: ""; }
.connection-signal-field i { position: absolute; right: 0; left: 4%; height: 1px; background: linear-gradient(90deg, transparent, color-mix(in srgb, var(--ui-primary) 75%, transparent), transparent); transform-origin: 90% 50%; }
.connection-signal-field i:nth-child(1) { top: 32%; transform: rotate(8deg); }
.connection-signal-field i:nth-child(2) { top: 43%; transform: rotate(-4deg); }
.connection-signal-field i:nth-child(3) { top: 55%; transform: rotate(4deg); }
.connection-signal-field i:nth-child(4) { top: 67%; transform: rotate(-8deg); }
.connection-signal-field i:nth-child(5) { top: 50%; }
.connection-stage-aside { padding: 22px 24px; display: flex; flex-direction: column; border: 1px solid var(--ui-border); border-radius: 17px; background: color-mix(in srgb, var(--ui-surface-strong) 82%, transparent); }
.connection-stage-aside dl { margin: 0; }
.connection-stage-aside dl > div { padding: 13px 0; display: grid; gap: 5px; border-bottom: 1px solid var(--ui-border); }
.connection-stage-aside dt { color: var(--ui-text-muted); font-size: 11px; }
.connection-stage-aside dd { min-width: 0; margin: 0; color: var(--ui-text); font-size: 14px; font-weight: 560; overflow-wrap: anywhere; }
.connection-stage-aside .focus-fact-primary dd { color: var(--ui-primary); font-size: 30px; font-weight: 500; }
.connection-stage-aside .focus-fact-primary small { color: var(--ui-text-muted); font-size: 11px; }
.connection-stage-aside .is-tabular,
.connection-stage-aside .is-address { font-family: "v-mono", monospace; font-variant-numeric: tabular-nums; }
.connection-stage-aside .is-address { font-size: 11px; line-height: 1.45; }
.focus-open-button { min-height: 38px; margin-top: auto; padding: 0 14px; display: flex; align-items: center; justify-content: space-between; gap: 12px; border: 1px solid color-mix(in srgb, var(--ui-primary) 48%, var(--ui-border)); border-radius: 11px; background: color-mix(in srgb, var(--ui-primary) 8%, transparent); color: var(--ui-primary); cursor: pointer; transition: border-color 160ms var(--ui-ease-out), background-color 160ms var(--ui-ease-out), transform 140ms var(--ui-ease-out); }
.focus-open-button svg { width: 18px; height: 18px; }
.focus-open-button:active { transform: scale(.97); }

@keyframes connection-signal {
  0% { opacity: 0; transform: translateX(0); }
  12%, 88% { opacity: 1; }
  100% { opacity: 0; transform: translateX(calc(82cqw - 7px)); }
}
@keyframes online-pulse { 0%, 100% { opacity: .55; } 50% { opacity: 1; } }

@media (hover: hover) and (pointer: fine) {
  .focus-open-button:hover { border-color: var(--ui-primary); background: color-mix(in srgb, var(--ui-primary) 13%, transparent); }
}

@media (max-width: 1480px) and (min-width: 1051px) {
  .connection-stage { height: 455px; min-height: 455px; padding: 34px; grid-template-columns: minmax(420px, 1fr) 210px 235px; }
  .connection-stage h2 { font-size: 56px; }
  .connection-stage-main > strong { font-size: 42px; }
  .connection-stage-aside { padding: 18px 20px; }
}

@media (max-width: 1050px) {
  .connection-stage { grid-template-columns: minmax(0, 1fr) 250px; gap: 28px; }
  .connection-signal-field { display: none; }
  .connection-stage-aside { padding: 14px 16px; }
  .connection-stage-aside dl > div { grid-template-columns: 76px minmax(0, 1fr); }
}

@media (max-width: 820px) {
  .connection-stage { min-height: 0; padding: 24px 20px; grid-template-columns: minmax(0, 1fr); }
  .connection-stage h2 { font-size: clamp(36px, 12vw, 54px); }
  .connection-stage-main > strong { font-size: 34px; }
  .connection-stage-main > p { font-size: 16px; }
  .connection-path { margin-top: 42px; }
  .connection-stage-aside { margin-top: 26px; padding: 18px; }
  .connection-stage-aside dl { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); }
  .connection-stage-aside dl > div { min-width: 0; padding: 0 14px; grid-template-columns: minmax(0, 1fr); border-bottom: 0; border-left: 1px solid var(--ui-border); }
  .connection-stage-aside dl > div:first-child { padding-left: 0; border-left: 0; }
  .connection-stage-aside dd { font-size: 12px; }
  .connection-stage-aside .focus-fact-primary dd { font-size: 20px; }
  .focus-open-button { display: none; }
}

@media (max-width: 560px) {
  .connection-stage-aside dl { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 18px 0; }
  .connection-stage-aside dl > div:nth-child(3) { padding-left: 0; border-left: 0; }
}

@media (prefers-reduced-motion: reduce) {
  .focus-device-status.is-online i,
  .connection-path.is-flowing .connection-signal { animation: none; }
  .connection-path.is-flowing .connection-signal { opacity: .7; transform: none; }
  .focus-open-button { transition-duration: 0ms; }
}
</style>
