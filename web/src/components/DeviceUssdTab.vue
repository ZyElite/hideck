<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { ArrowDown24Regular, ArrowUp24Regular, Phone24Regular, Send24Regular } from '@vicons/fluent'
import { devicesService } from '../services/devices'
import { formatDeviceTime } from '../utils/deviceTime'
import { createDeviceRequestScope } from '../utils/deviceRequestScope'
import StatusLight from './StatusLight.vue'

const props = defineProps<{
  deviceId: string
  vowifiActive?: boolean
}>()

const ussdCmd = ref('')
const ussdTimeoutMs = ref(45000)
const sending = ref(false)
const sessionId = ref('')
const sessionChannel = ref('')
const history = ref<Array<{ ts: number; type: 'req' | 'res' | 'err' | 'sys'; content: string; dcs?: number; channel?: string }>>([])
const requestScope = createDeviceRequestScope(props.deviceId)

const isMultiRound = computed(() => !!sessionId.value)
const inputPlaceholder = computed(() => isMultiRound.value ? '输入菜单选项数字' : '例如 *100# 或菜单回复数字')
const requestHistory = computed(() => history.value
  .filter(item => item.type === 'req')
  .slice()
  .reverse()
  .slice(0, 8))

watch(() => props.deviceId, (deviceId) => {
  requestScope.invalidate(deviceId)
  ussdCmd.value = ''
  sending.value = false
  history.value = []
  endSession()
})

async function sendUSSD() {
  const cmd = String(ussdCmd.value || '').trim()
  if (!cmd) return
  const deviceId = props.deviceId
  const requestToken = requestScope.begin(deviceId)
  
  history.value.push({ ts: Date.now(), type: 'req', content: cmd })
  sending.value = true
  ussdCmd.value = ''

  try {
    let d: { status?: number; text?: string; rawText?: string; dcs?: number; sessionId?: string; channel?: string }

    if (isMultiRound.value) {
      // 多轮模式：通过 continue 接口发送后续输入
      const result = await devicesService.continueUSSD(deviceId, {
        session_id: sessionId.value,
        input: cmd,
        timeout_ms: ussdTimeoutMs.value || 45000
      }, (ussdTimeoutMs.value || 45000) + 2000)
      if (!result.ok) throw new Error(result.error.message || '请求异常')
      d = result.data
    } else {
      // 首轮模式：发起新 USSD 请求
      const result = await devicesService.sendUSSD(deviceId, {
        command: cmd,
        timeout_ms: ussdTimeoutMs.value || 45000
      }, (ussdTimeoutMs.value || 45000) + 2000)
      if (!result.ok) throw new Error(result.error.message || '请求异常')
      d = result.data
    }

    if (!requestScope.isCurrent(requestToken, props.deviceId)) return

    // 更新通道和会话信息
    if (d.channel) sessionChannel.value = d.channel

    if (d.status === 5) {
      history.value.push({
        ts: Date.now(),
        type: 'err',
        content: `[网络不支持/无响应]\n` + (d.text || d.rawText || '[空响应]'),
        dcs: d.dcs,
        channel: d.channel
      })
      endSession()
    } else if (d.status === 2) {
      history.value.push({
        ts: Date.now(),
        type: 'err',
        content: `[被网络终止]\n` + (d.text || d.rawText || '[空响应]'),
        dcs: d.dcs,
        channel: d.channel
      })
      endSession()
    } else {
      history.value.push({
        ts: Date.now(),
        type: 'res',
        content: d.text || d.rawText || '[空响应]',
        dcs: d.dcs,
        channel: d.channel
      })
      // status=1 表示网络期望后续输入（多轮）
      if (d.status === 1 && d.sessionId) {
        sessionId.value = d.sessionId
      } else {
        endSession()
      }
    }
  } catch (e: unknown) {
    if (!requestScope.isCurrent(requestToken, props.deviceId)) return
    history.value.push({
      ts: Date.now(),
      type: 'err',
      content: e instanceof Error ? e.message : '请求异常'
    })
    endSession()
  } finally {
    if (requestScope.isCurrent(requestToken, props.deviceId)) {
      sending.value = false
    }
  }
}

async function cancelSession() {
  if (!sessionId.value) return
  const deviceId = props.deviceId
  const activeSessionId = sessionId.value
  const requestToken = requestScope.begin(deviceId)
  try {
    const result = await devicesService.cancelUSSD(deviceId, activeSessionId)
    if (!requestScope.isCurrent(requestToken, props.deviceId)) return
    if (!result.ok) throw new Error(result.error.message || '取消会话失败')
    history.value.push({
      ts: Date.now(),
      type: 'sys',
      content: '会话已手动取消'
    })
    endSession()
  } catch (error: unknown) {
    if (!requestScope.isCurrent(requestToken, props.deviceId)) return
    history.value.push({
      ts: Date.now(),
      type: 'err',
      content: error instanceof Error ? error.message : '取消会话失败'
    })
  }
}

function endSession() {
  sessionId.value = ''
  sessionChannel.value = ''
}

function clearHistory() {
  history.value = []
  endSession()
}

function selectHistory(command: string) {
  ussdCmd.value = command
}
</script>

<template>
  <section class="ussd-workspace">
    <header class="ussd-workspace-header">
      <div class="ussd-heading">
        <div class="ussd-heading-icon" aria-hidden="true">
          <el-icon><Phone24Regular /></el-icon>
        </div>
        <div>
          <span>CARRIER SESSION</span>
          <h2>USSD 终端</h2>
          <p>发送 USSD 代码并等待网络菜单响应</p>
        </div>
      </div>
      <div class="ussd-header-state">
        <StatusLight :tone="isMultiRound ? 'success' : 'neutral'" size="sm" :animated="isMultiRound" />
        {{ isMultiRound ? '多轮会话中' : '等待会话' }}
      </div>
    </header>

    <div class="ussd-composer">
      <div class="ussd-command-field">
        <label for="ussd-command">{{ isMultiRound ? '菜单回复' : 'USSD 代码' }}</label>
        <el-input id="ussd-command" v-model="ussdCmd" :placeholder="inputPlaceholder" @keyup.enter="sendUSSD" :disabled="sending" />
      </div>
      <div class="ussd-timeout-field">
        <label for="ussd-timeout">超时 (ms)</label>
        <el-input id="ussd-timeout" v-model.number="ussdTimeoutMs" type="number" inputmode="numeric" placeholder="45000" />
      </div>
      <el-button type="primary" :loading="sending" :disabled="!ussdCmd" @click="sendUSSD" class="!border-0">
        <el-icon><Send24Regular /></el-icon>
        {{ isMultiRound ? '回复' : '发送' }}
      </el-button>
      <el-button v-if="isMultiRound" type="warning" plain @click="cancelSession" :disabled="sending">取消会话</el-button>
    </div>

    <div class="ussd-content-layout">
      <div class="ussd-thread" aria-live="polite" aria-label="USSD 会话消息">
        <div v-if="history.length === 0 && !sending" class="ussd-thread-empty">
          <span>暂无 USSD 会话记录</span>
          <small>发送真实代码后，网络响应会显示在这里</small>
        </div>
        <article v-for="(msg, i) in history" :key="i" class="ussd-message" :class="`is-${msg.type}`">
          <div class="ussd-message-icon" aria-hidden="true">
            <ArrowUp24Regular v-if="msg.type === 'req'" />
            <ArrowDown24Regular v-else />
          </div>
          <div>
            <strong>{{ msg.type === 'req' ? '发送' : msg.type === 'res' ? '网络回复' : msg.type === 'err' ? '错误' : '系统' }}</strong>
            <p>{{ msg.content }}</p>
            <small>
              {{ formatDeviceTime(msg.ts, { clientClock: true }) }}
              <template v-if="msg.dcs !== undefined"> · DCS {{ msg.dcs }}</template>
              <template v-if="msg.channel"> · {{ msg.channel === 'vowifi' ? 'VoWiFi' : 'CS' }}</template>
            </small>
          </div>
        </article>
        <div v-if="sending" class="ussd-pending">
          <span /><span /><span />
          <small>等待网络响应...</small>
        </div>
      </div>

      <aside class="ussd-history-rail">
        <header>
          <h3>本次历史</h3>
          <button type="button" :disabled="history.length === 0" @click="clearHistory">清空</button>
        </header>
        <p v-if="requestHistory.length === 0">尚无已发送代码</p>
        <button v-for="item in requestHistory" v-else :key="item.ts" type="button" @click="selectHistory(item.content)">
          <strong>{{ item.content }}</strong>
          <small>{{ formatDeviceTime(item.ts, { clientClock: true }) }}</small>
        </button>
      </aside>
    </div>
  </section>
</template>

<style scoped>
.ussd-workspace { overflow: hidden; border: 1px solid var(--ui-border); border-radius: 12px; background: var(--ui-surface-strong); }
.ussd-workspace-header,
.ussd-heading,
.ussd-header-state,
.ussd-composer,
.ussd-pending { display: flex; align-items: center; }
.ussd-workspace-header { min-height: 90px; padding: 18px 20px; justify-content: space-between; gap: 18px; border-bottom: 1px solid var(--ui-border); }
.ussd-heading { gap: 12px; }
.ussd-heading-icon { width: 44px; height: 44px; display: grid; place-items: center; border: 1px solid var(--ui-border); border-radius: 11px; color: var(--ui-primary); font-size: 22px; }
.ussd-heading span { color: var(--ui-primary); font: 700 var(--ui-font-caption) "v-mono", ui-monospace, monospace; letter-spacing: .15em; }
.ussd-heading h2 { margin: 3px 0 0; color: var(--ui-text); font-size: 20px; font-weight: 650; }
.ussd-heading p { margin: 3px 0 0; color: var(--ui-text-muted); font-size: var(--ui-font-body-sm); }
.ussd-header-state { gap: 6px; color: var(--ui-text-muted); font-size: var(--ui-font-caption); }

.ussd-composer { padding: 14px 16px; gap: 10px; border-bottom: 1px solid var(--ui-border); background: color-mix(in srgb, var(--ui-primary) 4%, var(--ui-surface)); }
.ussd-command-field { min-width: 180px; flex: 1 1 auto; }
.ussd-timeout-field { width: 118px; flex: 0 0 auto; }
.ussd-composer label { display: block; margin-bottom: 4px; color: var(--ui-text-muted); font: 700 var(--ui-font-caption) "v-mono", ui-monospace, monospace; letter-spacing: .08em; }
.ussd-composer :deep(.el-button + .el-button) { margin-left: 0; }
.ussd-composer > .el-button { align-self: flex-end; }

.ussd-content-layout { min-height: 390px; display: grid; grid-template-columns: minmax(0, 1fr) 210px; }
.ussd-thread { position: relative; max-height: 460px; padding: 18px; overflow: auto; border-right: 1px solid var(--ui-border); }
.ussd-thread-empty { position: absolute; inset: 0; display: grid; place-content: center; gap: 4px; color: var(--ui-text-muted); text-align: center; }
.ussd-thread-empty small { font-size: var(--ui-font-caption); }
.ussd-message { padding: 12px 0; display: grid; grid-template-columns: 30px minmax(0, 1fr); gap: 10px; border-bottom: 1px solid var(--ui-border-muted); }
.ussd-message-icon { width: 28px; height: 28px; display: grid; place-items: center; border-radius: 50%; background: color-mix(in srgb, var(--ui-primary) 9%, var(--ui-surface)); color: var(--ui-primary); }
.ussd-message-icon svg { width: 14px; height: 14px; }
.ussd-message strong { color: var(--ui-text); font-size: var(--ui-font-body-sm); }
.ussd-message p { margin: 5px 0 0; color: var(--ui-text); font: var(--ui-font-body-sm)/1.55 "v-mono", ui-monospace, monospace; white-space: pre-wrap; overflow-wrap: anywhere; }
.ussd-message small { color: var(--ui-text-muted); font-size: var(--ui-font-caption); }
.ussd-message.is-err .ussd-message-icon,
.ussd-message.is-err strong { color: var(--ui-danger); }
.ussd-message.is-sys .ussd-message-icon,
.ussd-message.is-sys strong { color: var(--ui-warning); }
.ussd-pending { margin-top: 14px; gap: 5px; color: var(--ui-primary); }
.ussd-pending > span { width: 5px; height: 5px; border-radius: 50%; background: currentColor; animation: ussd-pending 1.1s ease-in-out infinite; }
.ussd-pending > span:nth-child(2) { animation-delay: 120ms; }
.ussd-pending > span:nth-child(3) { animation-delay: 240ms; }
.ussd-pending small { margin-left: 5px; color: var(--ui-text-muted); }

.ussd-history-rail { min-width: 0; padding: 16px 10px; background: color-mix(in srgb, var(--ui-surface-muted) 70%, var(--ui-surface)); }
.ussd-history-rail header { padding: 0 6px 10px; display: flex; align-items: center; justify-content: space-between; }
.ussd-history-rail h3 { margin: 0; color: var(--ui-text); font-size: var(--ui-font-body-sm); }
.ussd-history-rail header button { border: 0; background: transparent; color: var(--ui-text-muted); font-size: var(--ui-font-caption); cursor: pointer; }
.ussd-history-rail > p { margin: 12px 6px; color: var(--ui-text-muted); font-size: var(--ui-font-caption); }
.ussd-history-rail > button { width: 100%; padding: 10px 9px; display: flex; align-items: flex-start; justify-content: space-between; gap: 8px; border: 0; border-radius: 7px; background: transparent; color: var(--ui-text); text-align: left; cursor: pointer; }
.ussd-history-rail > button:hover { background: var(--ui-surface-muted); }
.ussd-history-rail > button strong { min-width: 0; overflow: hidden; font: var(--ui-font-body-sm) "v-mono", ui-monospace, monospace; text-overflow: ellipsis; white-space: nowrap; }
.ussd-history-rail > button small { flex: 0 0 auto; color: var(--ui-text-muted); font-size: var(--ui-font-caption); }

@keyframes ussd-pending { 0%, 100% { opacity: .35; transform: translateY(0); } 50% { opacity: 1; transform: translateY(-2px); } }

@media (max-width: 760px) {
  .ussd-workspace-header { align-items: flex-start; flex-direction: column; }
  .ussd-composer { align-items: stretch; flex-wrap: wrap; }
  .ussd-command-field { flex-basis: 100%; }
  .ussd-content-layout { grid-template-columns: minmax(0, 1fr); }
  .ussd-thread { min-height: 340px; border-right: 0; border-bottom: 1px solid var(--ui-border); }
  .ussd-history-rail { min-height: 120px; }
}

@media (max-width: 520px) {
  .ussd-workspace-header { padding: 16px; }
  .ussd-composer { padding: 12px; }
  .ussd-timeout-field { width: 100%; }
  .ussd-composer > .el-button { min-height: 44px; flex: 1 1 calc(50% - 5px); }
}

@media (prefers-reduced-motion: reduce) {
  .ussd-pending > span { animation: none; opacity: .7; }
}
</style>
