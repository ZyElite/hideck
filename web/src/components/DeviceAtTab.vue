<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ArrowDownload24Regular, Code24Regular, Send24Regular, Warning24Regular } from '@vicons/fluent'
import { AT_TEMPLATES } from '../constants/atTemplates'
import { devicesService } from '../services/devices'
import { formatDeviceTime } from '../utils/deviceTime'
import { createDeviceRequestScope } from '../utils/deviceRequestScope'
import StatusLight from './StatusLight.vue'

const props = defineProps<{
  deviceId: string
  backendMode?: string
  atPort?: string
  running?: boolean
}>()

const atCmd = ref('')
const atTemplate = ref('')
const atTimeoutMs = ref(10000)
const atSending = ref(false)
const atHistory = ref<Array<{ ts: number; cmd: string; ok: boolean; response: string }>>([])
const requestScope = createDeviceRequestScope(props.deviceId)

const atTemplates = AT_TEMPLATES
const quickCommands = computed(() => AT_TEMPLATES[0]?.items.slice(0, 4) || [])
const hasATPort = computed(() => String(props.atPort || '').trim().length > 0)
const canUseATTerminal = computed(() => Boolean(props.running) && hasATPort.value)
const unavailableTitle = computed(() => {
  if (!props.running) return '当前设备未运行'
  if (!hasATPort.value) return '当前设备没有可用 AT 口'
  return 'AT 终端暂不可用'
})
const unavailableDescription = computed(() => {
  if (!props.running) {
    return '设备当前未启动，AT 终端暂时不可用。待设备运行后，如果存在可用的 AT 口，即可在这里直接发送 AT 指令。'
  }
  if (!hasATPort.value && props.backendMode === 'qmi') {
    return '设备当前处于纯 QMI 模式，但没有解析到可用的 AT 口，因此无法提供 AT 串口终端。'
  }
  if (!hasATPort.value) {
    return '设备当前没有可用的 AT 口，因此无法提供 AT 串口终端。'
  }
  return '当前设备暂时无法提供 AT 串口终端，请稍后重试。'
})

watch(
  () => atTemplate.value,
  (v) => {
    const cmd = String(v || '').trim()
    if (cmd) atCmd.value = cmd
  }
)

watch(() => props.deviceId, (deviceId) => {
  requestScope.invalidate(deviceId)
  atCmd.value = ''
  atTemplate.value = ''
  atSending.value = false
  atHistory.value = []
})

async function sendAT() {
  const cmd = String(atCmd.value || '').trim()
  if (!cmd) return
  const deviceId = props.deviceId
  const requestToken = requestScope.begin(deviceId)
  atSending.value = true
  atCmd.value = '' // 清空输入框
  try {
    const result = await devicesService.sendAT(deviceId, {
      cmd: cmd,
      timeout_ms: atTimeoutMs.value || 10000
    })
    if (!requestScope.isCurrent(requestToken, props.deviceId)) return
    if (!result.ok) throw new Error(result.error.message || '请求异常')
    atHistory.value.push({
      ts: Date.now(),
      cmd,
      ok: result.data.ok,
      response: result.data.response
    })
  } catch (e: unknown) {
    if (!requestScope.isCurrent(requestToken, props.deviceId)) return
    atHistory.value.push({
      ts: Date.now(),
      cmd,
      ok: false,
      response: e instanceof Error ? e.message : '请求异常'
    })
  } finally {
    if (requestScope.isCurrent(requestToken, props.deviceId)) {
      atSending.value = false
    }
  }
}

function clearATHistory() {
  atHistory.value = []
}

function selectQuickCommand(command: string) {
  atCmd.value = command
}

function exportATHistory() {
  if (atHistory.value.length === 0) return
  const content = atHistory.value.map((entry) => [
    `[${formatDeviceTime(entry.ts, { clientClock: true })}] > ${entry.cmd}`,
    `${entry.ok ? 'RESPONSE' : 'ERROR'}\n${entry.response}`
  ].join('\n')).join('\n\n')
  const url = URL.createObjectURL(new Blob([content], { type: 'text/plain;charset=utf-8' }))
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `${props.deviceId}-at-session.txt`
  anchor.click()
  URL.revokeObjectURL(url)
}
</script>

<template>
  <section class="at-workspace">
    <header class="at-workspace-header">
      <div class="at-terminal-status">
        <div class="at-terminal-icon" aria-hidden="true">
          <el-icon><Code24Regular /></el-icon>
        </div>
        <div>
          <span>AT TERMINAL</span>
          <h2>{{ atPort || 'AT 端口不可用' }}</h2>
          <p>
            <StatusLight :tone="canUseATTerminal ? 'success' : 'danger'" size="sm" :animated="canUseATTerminal" />
            {{ canUseATTerminal ? '已连接' : unavailableTitle }}
          </p>
        </div>
      </div>
      <div class="at-header-actions">
        <el-button :disabled="atHistory.length === 0" @click="exportATHistory" class="ui-glass-border !border-0">
          <el-icon><ArrowDownload24Regular /></el-icon>
          导出
        </el-button>
        <el-button :disabled="atHistory.length === 0" @click="clearATHistory" class="ui-glass-border !border-0">清屏</el-button>
      </div>
    </header>

    <template v-if="!canUseATTerminal">
      <div class="at-unavailable-state">
        <el-icon size="48" class="text-orange-400 mb-4"><Warning24Regular /></el-icon>
        <div class="text-lg font-bold text-orange-700 dark:text-orange-400">{{ unavailableTitle }}</div>
        <div class="text-sm text-orange-600 dark:text-orange-300 mt-2 text-center max-w-md">
          {{ unavailableDescription }}
        </div>
      </div>
    </template>
    
    <template v-else>
      <p class="at-session-notice">AT 指令会发送到当前设备的真实端口；多行响应和错误会完整保留在本次页面会话中。</p>

      <div class="at-terminal-output" aria-live="polite" aria-label="AT 终端输出">
      <div v-if="atHistory.length === 0 && !atSending" class="at-terminal-empty">
        <span>VoHive AT Console</span>
        <small>输入命令后，真实设备响应将显示在这里</small>
      </div>
      <div v-for="(h, i) in atHistory" :key="h.ts + h.cmd + i" class="at-terminal-entry">
        <div class="at-terminal-command"><span>&gt;</span>{{ h.cmd }}</div>
        <pre :class="{ 'is-error': !h.ok }">{{ h.response }}</pre>
        <small>{{ h.ok ? '响应' : '错误' }} · {{ formatDeviceTime(h.ts, { clientClock: true }) }}</small>
      </div>

      <div v-if="atSending" class="at-terminal-pending">
        <span /><span /><span />
        <small>等待模组响应...</small>
      </div>
      </div>

    <div class="at-command-composer">
      <div class="space-y-1">
        <div class="text-xs font-bold text-gray-500 uppercase tracking-wider">快捷指令模板</div>
        <el-select v-model="atTemplate" filterable clearable placeholder="选择常用命令（可选）">
           <el-option-group v-for="g in atTemplates" :key="g.label" :label="g.label">
            <el-option v-for="it in g.items" :key="it.value" :label="it.label" :value="it.value" />
          </el-option-group>
        </el-select>
      </div>

      <div class="space-y-1">
        <div class="text-xs font-bold text-gray-500 uppercase tracking-wider">命令</div>
        <el-input
          v-model="atCmd"
          placeholder='例如 AT+CSQ (可自由编辑)'
          @keyup.enter="sendAT"
          :disabled="atSending"
        />
      </div>

      <div class="space-y-1">
        <div class="text-xs font-bold text-gray-500 uppercase tracking-wider">超时(ms)</div>
        <el-input v-model.number="atTimeoutMs" type="number" inputmode="numeric" placeholder="10000" />
      </div>

      <div class="space-y-1 self-end at-send-action">
        <div class="text-xs font-bold text-gray-500 uppercase tracking-wider opacity-0 select-none">操作</div>
        <div class="flex items-center justify-end gap-2">
          <el-button type="primary" :loading="atSending" :disabled="!atCmd" @click="sendAT" class="!border-0">
            <el-icon><Send24Regular /></el-icon>
            发送
          </el-button>
        </div>
      </div>
      <div class="at-quick-command-row">
        <span>快捷指令</span>
        <button v-for="item in quickCommands" :key="item.value" type="button" @click="selectQuickCommand(item.value)">
          {{ item.value }}
        </button>
      </div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.at-workspace {
  overflow: hidden;
  border: 1px solid var(--ui-border);
  border-radius: 12px;
  background: var(--ui-surface-strong);
}

.at-workspace-header,
.at-terminal-status,
.at-terminal-status p,
.at-header-actions,
.at-quick-command-row {
  display: flex;
  align-items: center;
}

.at-workspace-header {
  min-height: 82px;
  padding: 16px 18px;
  justify-content: space-between;
  gap: 18px;
  border-bottom: 1px solid var(--ui-border);
}

.at-terminal-status { gap: 12px; }
.at-terminal-icon { width: 42px; height: 42px; display: grid; place-items: center; border: 1px solid var(--ui-border); border-radius: 10px; color: var(--ui-primary); font-size: 21px; }
.at-terminal-status span { color: var(--ui-primary); font: 700 var(--ui-font-caption) "v-mono", ui-monospace, monospace; letter-spacing: .14em; }
.at-terminal-status h2 { margin: 3px 0 0; color: var(--ui-text); font: 600 15px "v-mono", ui-monospace, monospace; }
.at-terminal-status p { margin: 4px 0 0; gap: 6px; color: var(--ui-text-muted); font-size: var(--ui-font-caption); }

.at-session-notice {
  margin: 14px 16px 0;
  padding: 9px 11px;
  border: 1px solid color-mix(in srgb, var(--ui-primary) 22%, var(--ui-border));
  border-radius: 8px;
  background: color-mix(in srgb, var(--ui-primary) 5%, transparent);
  color: var(--ui-text-muted);
  font-size: var(--ui-font-body-sm);
}

.at-unavailable-state { margin: 16px; padding: 42px 24px; display: flex; align-items: center; justify-content: center; flex-direction: column; border: 1px solid color-mix(in srgb, var(--ui-warning) 30%, var(--ui-border)); border-radius: 10px; background: color-mix(in srgb, var(--ui-warning) 6%, var(--ui-surface)); }

.at-terminal-output {
  position: relative;
  height: 360px;
  margin: 14px 16px 0;
  padding: 16px;
  overflow: auto;
  border: 1px solid var(--ui-border);
  border-radius: 9px;
  background: #050706;
  color: #c8d5ce;
  font: var(--ui-font-body-sm)/1.6 "v-mono", ui-monospace, monospace;
}

.at-terminal-empty { position: absolute; inset: 0; display: grid; place-content: center; gap: 5px; color: #7d8c84; text-align: center; }
.at-terminal-empty span { color: #66e9ad; font-size: 13px; }
.at-terminal-entry { padding-bottom: 16px; }
.at-terminal-command { display: flex; gap: 8px; color: #f1f5f2; overflow-wrap: anywhere; }
.at-terminal-command span { color: #66e9ad; }
.at-terminal-entry pre { margin: 5px 0 0; color: #acb9b2; font: inherit; white-space: pre-wrap; overflow-wrap: anywhere; }
.at-terminal-entry pre.is-error { color: #ef8186; }
.at-terminal-entry small { color: #68746e; }
.at-terminal-pending { display: flex; align-items: center; gap: 5px; color: #66e9ad; }
.at-terminal-pending > span { width: 5px; height: 5px; border-radius: 50%; background: currentColor; animation: at-pending 1.1s ease-in-out infinite; }
.at-terminal-pending > span:nth-child(2) { animation-delay: 120ms; }
.at-terminal-pending > span:nth-child(3) { animation-delay: 240ms; }
.at-terminal-pending small { margin-left: 5px; color: #7d8c84; }

.at-command-composer { padding: 14px 16px 16px; display: grid; grid-template-columns: 200px minmax(180px, 1fr) 110px auto; gap: 12px; }
.at-quick-command-row { grid-column: 1 / -1; flex-wrap: wrap; gap: 7px; color: var(--ui-text-muted); font-size: var(--ui-font-caption); }
.at-quick-command-row button { min-height: 28px; padding: 0 9px; border: 1px solid var(--ui-border); border-radius: 6px; background: var(--ui-surface); color: var(--ui-text-muted); font: var(--ui-font-caption) "v-mono", ui-monospace, monospace; cursor: pointer; }

@keyframes at-pending { 0%, 100% { opacity: .35; transform: translateY(0); } 50% { opacity: 1; transform: translateY(-2px); } }

@media (max-width: 840px) {
  .at-command-composer { grid-template-columns: minmax(0, 1fr) 110px; }
  .at-command-composer > div:first-child { grid-column: 1 / -1; }
}

@media (max-width: 520px) {
  .at-workspace-header { align-items: flex-start; flex-direction: column; }
  .at-terminal-output { height: 300px; margin: 12px 10px 0; padding: 12px; }
  .at-session-notice { margin: 12px 10px 0; }
  .at-command-composer { padding: 12px 10px 14px; grid-template-columns: minmax(0, 1fr); }
  .at-command-composer > div:first-child,
  .at-quick-command-row { grid-column: auto; }
  .at-send-action :deep(.el-button) { width: 100%; min-height: 44px; }
}

@media (prefers-reduced-motion: reduce) {
  .at-terminal-pending > span { animation: none; opacity: .7; }
}
</style>
