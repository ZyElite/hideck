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
  device: DashboardDevice
}>()

const emit = defineEmits<{
  (event: 'open', deviceID: string): void
}>()

const presentation = computed(() => createDashboardDevicePresentation(props.device))
const showsConnectionPath = computed(() => props.device.vowifi_active === true)
const pathIsFlowing = computed(() => canAnimateDashboardConnection(props.device))

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
          :class="device.healthy ? 'is-online' : 'is-offline'"
        >
          <i aria-hidden="true" />
          {{ presentation.statusLabel }}
        </span>
      </header>

      <h2>{{ presentation.connectionTitle }}</h2>
      <strong>{{ presentation.connectionState }}</strong>
      <p>{{ presentation.displayName }} · {{ device.id }}</p>

      <div
        v-if="showsConnectionPath"
        class="connection-path"
        :class="{ 'is-flowing': pathIsFlowing }"
        aria-label="VoWiFi 服务链路"
      >
        <div class="connection-path-track" aria-hidden="true">
          <span class="connection-signal" />
        </div>
        <div
          v-for="stage in presentation.stages"
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
          <em>{{ stageStatusLabel(stage.ready) }}</em>
        </div>
      </div>

      <div v-else class="cellular-focus">
        <span>{{ presentation.connectionType }}</span>
        <strong>{{ presentation.signal }}</strong>
      </div>
    </div>

    <aside class="connection-stage-aside" aria-label="当前设备网络事实">
      <dl>
        <div>
          <dt>连接类型</dt>
          <dd>{{ presentation.connectionType }}</dd>
        </div>
        <div>
          <dt>运营商</dt>
          <dd>{{ presentation.operator }}</dd>
        </div>
        <div>
          <dt>信号</dt>
          <dd class="is-tabular">{{ presentation.signal }}</dd>
        </div>
        <div>
          <dt>公网 IPv4</dt>
          <dd class="is-address" :title="presentation.ipv4">{{ presentation.ipv4 }}</dd>
        </div>
        <div>
          <dt>公网 IPv6</dt>
          <dd class="is-address" :title="presentation.ipv6">{{ presentation.ipv6 }}</dd>
        </div>
      </dl>
      <button type="button" class="focus-open-button" @click="emit('open', device.id)">
        <span>打开设备工作区</span>
        <Open20Regular aria-hidden="true" />
      </button>
    </aside>
  </section>
</template>

<style scoped>
.connection-stage {
  position: relative;
  min-height: 410px;
  margin-bottom: 18px;
  padding: clamp(28px, 4vw, 52px);
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(270px, 320px);
  gap: clamp(32px, 4vw, 56px);
  overflow: hidden;
  border: 1px solid var(--ui-border);
  border-radius: 24px;
  background:
    radial-gradient(circle at 56% 51%, color-mix(in srgb, var(--ui-primary) 13%, transparent), transparent 31%),
    linear-gradient(132deg, var(--ui-surface) 0 48%, color-mix(in srgb, var(--ui-surface) 90%, #06120e) 100%);
  box-shadow: var(--ui-shadow-sm);
}

.connection-stage::before {
  position: absolute;
  inset: 0 27% 0 37%;
  opacity: .34;
  background-image: radial-gradient(circle, color-mix(in srgb, var(--ui-primary) 44%, transparent) 1px, transparent 1.4px);
  background-size: 18px 18px;
  mask-image: radial-gradient(ellipse, #000, transparent 70%);
  pointer-events: none;
  content: "";
}

.connection-stage-main,
.connection-stage-aside { position: relative; z-index: 1; min-width: 0; }
.connection-stage-heading { display: flex; align-items: center; gap: 12px; }
.dashboard-eyebrow { color: var(--ui-primary); font: 700 10px "v-mono", monospace; letter-spacing: .14em; }
.focus-device-status { display: inline-flex; align-items: center; gap: 7px; color: var(--ui-text-muted); font-size: 11px; }
.focus-device-status i { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
.focus-device-status.is-online { color: var(--ui-success); }
.focus-device-status.is-offline { color: var(--ui-danger); }
.focus-device-status.is-online i { animation: online-pulse 2.4s var(--ui-ease-in-out) infinite; }
.connection-stage h2 { margin: 26px 0 4px; color: var(--ui-text); font-size: clamp(40px, 5.4vw, 76px); font-weight: 560; letter-spacing: -.045em; line-height: .98; }
.connection-stage-main > strong { color: var(--ui-primary); font-size: clamp(23px, 3vw, 36px); font-weight: 540; }
.connection-stage-main > p { margin: 14px 0 0; color: var(--ui-text-muted); font-size: 13px; overflow-wrap: anywhere; }

.connection-path {
  position: relative;
  width: min(100%, 620px);
  margin-top: 58px;
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  container-type: inline-size;
}
.connection-path-track { position: absolute; top: 18px; right: 9%; left: 9%; height: 1px; background: var(--ui-border); }
.connection-path.is-flowing .connection-path-track { background: linear-gradient(90deg, color-mix(in srgb, var(--ui-primary) 34%, var(--ui-border)), var(--ui-primary), color-mix(in srgb, var(--ui-primary) 34%, var(--ui-border))); }
.connection-signal { position: absolute; top: -3px; left: 0; width: 7px; height: 7px; border-radius: 50%; opacity: 0; background: var(--ui-primary); box-shadow: 0 0 14px var(--ui-primary); }
.connection-path.is-flowing .connection-signal { animation: connection-signal 2.4s linear infinite; }
.connection-path-step { position: relative; z-index: 1; min-width: 0; display: grid; place-items: center; gap: 7px; color: var(--ui-text-muted); }
.connection-path-step > span { width: 37px; height: 37px; display: grid; place-items: center; border: 1px solid var(--ui-border); border-radius: 50%; background: var(--ui-surface-strong); color: inherit; }
.connection-path-step svg { width: 15px; height: 15px; }
.connection-path-step small { font-size: 10px; }
.connection-path-step em { max-width: 100%; overflow: hidden; font-size: 9px; font-style: normal; text-overflow: ellipsis; white-space: nowrap; }
.connection-path-step.is-ready { color: var(--ui-primary); }
.connection-path-step.is-ready > span { border-color: var(--ui-primary); box-shadow: 0 0 22px color-mix(in srgb, var(--ui-primary) 18%, transparent); }
.connection-path-step.is-failed { color: var(--ui-danger); }
.connection-path-step.is-failed > span { border-color: var(--ui-danger); }

.cellular-focus { width: min(100%, 520px); margin-top: 64px; padding-top: 24px; display: flex; align-items: end; justify-content: space-between; gap: 20px; border-top: 1px solid var(--ui-border); color: var(--ui-text-muted); }
.cellular-focus strong { color: var(--ui-text); font: 34px/1 "v-mono", monospace; text-align: right; }
.connection-stage-aside { padding: 18px 20px; display: flex; flex-direction: column; border: 1px solid var(--ui-border); border-radius: 17px; background: color-mix(in srgb, var(--ui-surface-strong) 82%, transparent); }
.connection-stage-aside dl { margin: 0; }
.connection-stage-aside dl > div { padding: 10px 0; display: grid; grid-template-columns: 90px minmax(0, 1fr); gap: 12px; border-bottom: 1px solid var(--ui-border-muted); }
.connection-stage-aside dt { color: var(--ui-text-muted); font-size: 11px; }
.connection-stage-aside dd { min-width: 0; margin: 0; color: var(--ui-text); font-size: 14px; font-weight: 560; text-align: right; overflow-wrap: anywhere; }
.connection-stage-aside .is-tabular,
.connection-stage-aside .is-address { font-family: "v-mono", monospace; font-variant-numeric: tabular-nums; }
.connection-stage-aside .is-address { font-size: 11px; line-height: 1.45; }
.focus-open-button { min-height: 44px; margin-top: auto; padding: 0 14px; display: flex; align-items: center; justify-content: space-between; gap: 12px; border: 1px solid color-mix(in srgb, var(--ui-primary) 48%, var(--ui-border)); border-radius: 11px; background: color-mix(in srgb, var(--ui-primary) 8%, transparent); color: var(--ui-primary); cursor: pointer; transition: border-color 160ms var(--ui-ease-out), background-color 160ms var(--ui-ease-out), transform 140ms var(--ui-ease-out); }
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

@media (max-width: 1050px) {
  .connection-stage { grid-template-columns: minmax(0, 1fr) 250px; gap: 28px; }
  .connection-stage-aside { padding: 14px 16px; }
  .connection-stage-aside dl > div { grid-template-columns: 76px minmax(0, 1fr); }
}

@media (max-width: 760px) {
  .connection-stage { min-height: 0; padding: 24px 20px; grid-template-columns: minmax(0, 1fr); }
  .connection-stage::before { inset: 0; }
  .connection-stage h2 { font-size: clamp(36px, 12vw, 54px); }
  .connection-path { margin-top: 42px; }
  .connection-path-step em { display: none; }
  .cellular-focus { margin-top: 42px; }
  .cellular-focus strong { font-size: 25px; }
  .connection-stage-aside { gap: 16px; }
  .focus-open-button { margin-top: 4px; }
}

@media (prefers-reduced-motion: reduce) {
  .focus-device-status.is-online i,
  .connection-path.is-flowing .connection-signal { animation: none; }
  .connection-path.is-flowing .connection-signal { opacity: .7; transform: none; }
  .focus-open-button { transition-duration: 0ms; }
}
</style>
