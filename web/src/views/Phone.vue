<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Backspace24Regular,
  Call24Regular,
  CallDismiss24Regular,
  CallEnd24Regular,
  Dialpad24Regular,
  LockClosed24Regular,
  Mic24Regular,
  MicOff24Regular,
  Speaker224Regular
} from '@vicons/fluent'
import PageHeader from '../components/PageHeader.vue'
import PhoneCallHistory from '../components/PhoneCallHistory.vue'
import PhoneDialPad from '../components/PhoneDialPad.vue'
import type { PhoneCall, PhoneDevice } from '../services/phone'
import { devicesService } from '../services/devices'
import { usePhoneStore } from '../stores/phone'
import { formatCallDuration, phoneCallStatusLabel, phoneErrorMessage } from '../utils/phone'

const CALLEE_PATTERN = /^\+?[0-9]{1,32}$/

const phone = usePhoneStore()
const selectedDevice = ref('')
const callee = ref('')
const action = ref('')
const keypadVisible = ref(false)
const lastDTMF = ref('')

const call = computed(() => phone.currentCall)
const callEnding = computed(() => call.value ? phone.isCallEnding(call.value.call_id) : false)
const connected = computed(() => call.value?.status === 'connected')
const incoming = computed(() => call.value?.direction === 'inbound'
  && call.value.status === 'ringing'
  && !call.value.media_id)
const selected = computed(() => phone.devices.find((device) => device.id === selectedDevice.value))
const canPlaceCall = computed(() => CALLEE_PATTERN.test(callee.value)
  && !!selected.value
  && (isDeviceReady(selected.value) || selected.value.phone_mode === 'cellular')
  && !isDeviceBusy(selected.value))
watch(() => phone.devices, (devices) => selectFirstAvailableDevice(devices), { immediate: true })
watch(call, (current) => {
  if (!current || current.status !== 'connected') keypadVisible.value = false
})

onMounted(async () => {
  if (!phone.initialized) await phone.initialize()
})

function selectFirstAvailableDevice(devices: PhoneDevice[]) {
  if (devices.some((device) => device.id === selectedDevice.value)) return
  selectedDevice.value = devices.find((device) => isDeviceReady(device))?.id || devices[0]?.id || ''
}

function isDeviceReady(device: PhoneDevice) {
  return device.voice.ready === true || device.voice.registered === true
}

function isDeviceBusy(device: PhoneDevice) {
  return phone.calls.some((item) => item.device_id === device.id)
}

function deviceModeLabel(device?: PhoneDevice) {
  return device?.phone_mode === 'cellular' ? '蜂窝数据' : 'WiFi calling'
}

function deviceStatus(device?: PhoneDevice) {
  if (!device) return '未选择设备'
  if (isDeviceBusy(device)) return '通话占用'
  const mode = deviceModeLabel(device)
  if (isDeviceReady(device) || device.vowifi_active) return `${mode} · 就绪`
  if (device.phone_mode === 'cellular' && device.vowifi_enabled) {
    if (!device.network_enabled && device.data_strategy !== 'always') return `${mode} · 驻网（未开流量）`
    return device.data_strategy === 'always' ? `${mode} · 连接中` : `${mode} · 仅打电话时开`
  }
  if (device.vowifi_enabled) return `${mode} · 连接中`
  return `${mode} · 未开启`
}

const selectedMode = computed(() => selected.value?.phone_mode === 'cellular' ? 'cellular' : 'wifi')
const selectedStrategy = computed(() => selected.value?.data_strategy === 'always' ? 'always' : 'on_demand')
const modePending = ref(false)

async function changePhoneMode(mode: string) {
  if (!selectedDevice.value || !!call.value || modePending.value) return
  if (
    mode === selectedMode.value
    && selected.value
    && (isDeviceReady(selected.value) || selected.value.vowifi_active)
  ) {
    return
  }
  modePending.value = true
  phone.clearError()
  try {
    const result = await devicesService.enableVoWiFi(selectedDevice.value, {
      mode,
      data_strategy: selectedStrategy.value
    })
    if (!result.ok) throw new Error(result.error?.message || '切换通话方式失败')
    await phone.refresh()
    if (mode === 'cellular') {
      ElMessage.success('已切到蜂窝。会正常驻网；要走流量再到卡策略打开「网络」')
    } else {
      ElMessage.success('已切换到 WiFi calling')
    }
  } catch (error) {
    phone.error = phoneErrorMessage(error, '切换通话方式失败')
  } finally {
    modePending.value = false
  }
}

async function changeDataStrategy(strategy: string) {
  if (!selectedDevice.value || !!call.value || modePending.value) return
  modePending.value = true
  phone.clearError()
  try {
    const result = await devicesService.enableVoWiFi(selectedDevice.value, {
      mode: 'cellular',
      data_strategy: strategy
    })
    if (!result.ok) throw new Error(result.error?.message || '切换数据策略失败')
    await phone.refresh()
    ElMessage.success(strategy === 'always'
      ? '已改为长时间开启数据。打开「网络」后会一直走流量'
      : '已改为仅打电话时开数据。打开「网络」后，拨号才会连流量')
  } catch (error) {
    phone.error = phoneErrorMessage(error, '切换数据策略失败')
  } finally {
    modePending.value = false
  }
}

function appendDigit(digit: string) {
  if (connected.value && keypadVisible.value) {
    void sendDTMF(digit)
    return
  }
  if (callee.value.length < 32) callee.value += digit
}

function eraseDigit() {
  callee.value = callee.value.slice(0, -1)
}

async function runAction(name: string, task: () => Promise<unknown>, success?: string) {
  if (action.value) return
  action.value = name
  phone.clearError()
  try {
    await task()
    if (success) ElMessage.success(success)
  } catch (error) {
    phone.error = phoneErrorMessage(error, `${name}失败`)
  } finally {
    action.value = ''
  }
}

function enableMedia() {
  return runAction('启用听筒', () => phone.enableMedia(), '听筒和麦克风已启用')
}

function enableListenOnlyMedia() {
  return runAction('恢复仅听', () => phone.enableListenOnlyMedia(), '仅听模式已恢复')
}

function startListenOnlyCall() {
  if (!canPlaceCall.value) return
  if (selected.value?.phone_mode === 'cellular') {
    return runAction('仅听呼叫', async () => {
      const confirmed = await ElMessageBox.confirm(
        selected.value?.network_enabled
          ? '当前用蜂窝数据打电话，会消耗少量流量。是否继续？'
          : '当前未开流量。打这个电话会临时打开数据，挂断后关闭。是否继续？',
        '数据流量通话提醒',
        { confirmButtonText: '继续拨号', cancelButtonText: '取消', type: 'warning' }
      ).then(() => true).catch(() => false)
      if (!confirmed) return
      return phone.startListenOnlyCall(selectedDevice.value, callee.value)
    })
  }
  return runAction('仅听呼叫', () => phone.startListenOnlyCall(selectedDevice.value, callee.value))
}

function startTwoWayCall() {
  if (!canPlaceCall.value || !phone.secureContext) return
  if (selected.value?.phone_mode === 'cellular') {
    return runAction('双向呼叫', async () => {
      const confirmed = await ElMessageBox.confirm(
        selected.value?.network_enabled
          ? '当前用蜂窝数据打电话，会消耗少量流量。是否继续？'
          : '当前未开流量。打这个电话会临时打开数据，挂断后关闭。是否继续？',
        '数据流量通话提醒',
        { confirmButtonText: '继续拨号', cancelButtonText: '取消', type: 'warning' }
      ).then(() => true).catch(() => false)
      if (!confirmed) return
      return phone.startCall(selectedDevice.value, callee.value)
    })
  }
  return runAction('双向呼叫', () => phone.startCall(selectedDevice.value, callee.value))
}

function answerCall(current: PhoneCall) {
  return runAction('麦克风接听', () => phone.answer(current))
}

function answerListenOnly(current: PhoneCall) {
  return runAction('仅听接听', () => phone.answerListenOnly(current))
}

function rejectCall(current: PhoneCall) {
  return runAction('拒接', () => phone.reject(current))
}

function hangup(current: PhoneCall) {
  return runAction('挂断', () => phone.hangup(current), '已发送挂断请求')
}

function takeOver(current: PhoneCall) {
  return runAction('接管', () => phone.takeOver(current), '已接管这通电话')
}

async function sendDTMF(digit: string) {
  if (!connected.value) return
  try {
    await phone.sendDTMF(digit)
    lastDTMF.value = digit
  } catch (error) {
    phone.error = phoneErrorMessage(error, 'DTMF 发送失败')
  }
}
</script>

<template>
  <div class="app-page phone-page">
    <PageHeader title="电话" subtitle="通过 VoWiFi 设备进行浏览器实时语音与 DTMF 通话">
      <template #actions>
        <span class="media-state" :class="`is-${phone.mediaState}`">
          <span aria-hidden="true" />{{ phone.mediaReady
            ? phone.mediaMode === 'listen-only' ? '仅听已连接' : '双向语音已连接'
            : '听筒未连接' }}
        </span>
      </template>
    </PageHeader>

    <section v-if="!phone.secureContext" class="security-notice" role="alert">
      <div class="notice-icon"><el-icon><LockClosed24Regular /></el-icon></div>
      <div>
        <strong>麦克风需要受信任的 HTTPS</strong>
        <p>当前 HTTP 页面可“仅听接听”或“仅听呼叫”，不会申请麦克风，因此对方听不到你。请通过已配置的 Nginx、Caddy 或其他受信任 HTTPS 地址访问后再使用双向通话。</p>
      </div>
    </section>

    <div v-if="phone.error || phone.eventError" class="phone-error" role="alert">
      <span>{{ phone.error || phone.eventError }}</span>
      <button type="button" aria-label="关闭错误提示" @click="phone.clearError(); phone.eventError = ''">×</button>
    </div>

    <section class="phone-workspace ui-card ui-workspace-glow">
      <div class="phone-grid">
      <section class="phone-console" aria-labelledby="phone-console-title">
        <header class="console-header">
          <div>
            <span>WEBRTC HANDSET</span>
            <h2 id="phone-console-title">{{ call ? '当前电话' : '拨号' }}</h2>
          </div>
          <div class="device-selector">
            <label for="phone-device">语音设备</label>
            <el-select
              id="phone-device"
              v-model="selectedDevice"
              aria-label="语音设备"
              placeholder="选择语音设备"
              :disabled="!!call"
              popper-class="phone-device-dropdown"
            >
              <el-option v-if="!phone.devices.length" label="无可用设备" value="" />
              <el-option
                v-for="device in phone.devices"
                :key="device.id"
                :label="`${device.name || device.id} · ${deviceStatus(device)}`"
                :value="device.id"
                :disabled="isDeviceBusy(device)"
              />
            </el-select>
            <div v-if="selectedDevice" class="phone-mode-bar">
              <el-radio-group
                :model-value="selectedMode"
                size="small"
                :disabled="!!call || modePending"
              >
                <el-radio-button value="wifi" @click="void changePhoneMode('wifi')">WiFi calling</el-radio-button>
                <el-radio-button value="cellular" @click="void changePhoneMode('cellular')">蜂窝数据</el-radio-button>
              </el-radio-group>
              <el-select
                v-if="selectedMode === 'cellular'"
                :model-value="selectedStrategy"
                size="small"
                style="width: 160px"
                :disabled="!!call || modePending"
                @change="(val: string | number | boolean | undefined) => { if (typeof val === 'string') void changeDataStrategy(val) }"
              >
                <el-option label="仅打电话时开" value="on_demand" />
                <el-option label="长时间开启" value="always" />
              </el-select>
              <p v-if="selectedMode === 'cellular'" class="phone-mode-hint">
                {{ selected?.network_enabled
                  ? (selectedStrategy === 'always' ? '网络已开，数据会保持连接。' : '网络已开，只有拨号时才连数据，挂断后关闭。')
                  : '会正常驻网，待机不走流量。打蜂窝电话会临时打开数据。' }}
              </p>
              <p v-else-if="selected && !isDeviceReady(selected) && !selected.vowifi_active" class="phone-mode-hint">
                软件电话未开启，点 WiFi calling 即可拉起
              </p>
            </div>
          </div>
        </header>

        <div v-if="phone.loading" class="console-loading">正在加载电话状态…</div>

        <div v-else-if="call" class="active-call">
          <div class="call-identity">
            <span>{{ call.direction === 'inbound' ? 'INCOMING' : 'OUTGOING' }}</span>
            <strong>{{ call.peer || '未知号码' }}</strong>
            <p>{{ phoneCallStatusLabel(call, callEnding) }} · {{ formatCallDuration(call, phone.now) }}</p>
          </div>

          <dl class="call-meta">
            <div><dt>设备</dt><dd>{{ call.device_id }}</dd></div>
            <div><dt>Codec</dt><dd>{{ call.codec || '协商中' }}</dd></div>
            <div>
              <dt>媒体</dt>
              <dd>{{ phone.mediaReady
                ? phone.mediaMode === 'listen-only' ? '仅听' : '双向'
                : call.read_only ? '其他浏览器控制' : '等待恢复' }}</dd>
            </div>
          </dl>

          <div v-if="incoming" class="incoming-actions">
            <button type="button" class="round-action is-reject" :disabled="!!action" @click="rejectCall(call)">
              <el-icon><CallDismiss24Regular /></el-icon><span>拒接</span>
            </button>
            <button
              type="button"
              class="round-action is-listen"
              :disabled="!!action"
              title="只接收对方声音，不申请麦克风"
              @click="answerListenOnly(call)"
            >
              <el-icon><Speaker224Regular /></el-icon><span>仅听接听</span><small>对方听不到你</small>
            </button>
            <button
              type="button"
              class="round-action is-answer"
              :disabled="!!action || !phone.secureContext"
              title="需要受信任的 HTTPS 和麦克风权限"
              @click="answerCall(call)"
            >
              <el-icon><Call24Regular /></el-icon><span>麦克风接听</span>
            </button>
          </div>

          <div v-else-if="call.read_only" class="takeover-panel">
            <strong>此电话由另一个浏览器控制</strong>
            <p>当前只能查看状态。显式接管会断开原浏览器媒体并把控制租约转移到本标签页。</p>
            <button type="button" class="secondary-button" :disabled="!!action || !phone.secureContext" @click="takeOver(call)">
              接管电话
            </button>
          </div>

          <template v-else>
            <div v-if="!phone.mediaReady" class="restore-actions">
              <button
                type="button"
                class="restore-button"
                :disabled="!!action || callEnding"
                @click="enableListenOnlyMedia"
              >
                <el-icon><Speaker224Regular /></el-icon>在 15 秒内恢复仅听
              </button>
              <button
                type="button"
                class="restore-button"
                :disabled="!!action || callEnding || !phone.secureContext"
                @click="enableMedia"
              >
                <el-icon><Mic24Regular /></el-icon>恢复双向语音
              </button>
            </div>

            <div v-else-if="phone.mediaMode === 'listen-only'" class="listen-only-panel" role="status">
              <div>
                <strong>仅听模式</strong>
                <p>你能听到对方，对方听不到你；浏览器没有申请麦克风。</p>
              </div>
              <button
                type="button"
                class="secondary-button"
                :disabled="!!action || callEnding || !phone.secureContext"
                @click="enableMedia"
              >
                <el-icon><Mic24Regular /></el-icon>{{ phone.secureContext ? '启用麦克风' : 'HTTPS 下可启用麦克风' }}
              </button>
            </div>

            <div v-if="connected && keypadVisible" class="active-keypad">
              <p aria-live="polite">发送 DTMF{{ lastDTMF ? `：${lastDTMF}` : '' }}</p>
              <PhoneDialPad :disabled="!!action || callEnding" @digit="appendDigit" />
            </div>

            <div class="call-controls" aria-label="通话控制">
              <button
                type="button"
                class="control-button"
                :disabled="callEnding || !phone.mediaReady || phone.mediaMode !== 'two-way'"
                :aria-pressed="phone.muted"
                @click="phone.toggleMute"
              >
                <el-icon>
                  <Speaker224Regular v-if="phone.mediaMode === 'listen-only'" />
                  <MicOff24Regular v-else-if="phone.muted" />
                  <Mic24Regular v-else />
                </el-icon>
                <span>{{ phone.mediaMode === 'listen-only' ? '仅听模式' : phone.muted ? '取消静音' : '静音' }}</span>
              </button>
              <button
                type="button"
                class="control-button"
                :disabled="callEnding || !connected"
                :aria-pressed="keypadVisible"
                @click="keypadVisible = !keypadVisible"
              >
                <el-icon><Dialpad24Regular /></el-icon><span>键盘</span>
              </button>
              <button type="button" class="control-button is-hangup" :disabled="!!action || callEnding" @click="hangup(call)">
                <el-icon><CallEnd24Regular /></el-icon><span>{{ action === '挂断' || callEnding ? '挂断中…' : '挂断' }}</span>
              </button>
            </div>
          </template>
        </div>

        <div v-else class="dialer">
          <div class="selected-device-status" :class="{ 'is-ready': selected && isDeviceReady(selected) }">
            <span aria-hidden="true" />{{ deviceStatus(selected) }}
          </div>

          <div class="number-field">
            <label for="callee">电话号码</label>
            <div>
              <input
                id="callee"
                v-model.trim="callee"
                type="tel"
                inputmode="tel"
                maxlength="32"
                autocomplete="tel"
                placeholder="输入号码"
              />
              <button type="button" aria-label="删除末位号码" :disabled="!callee" @click="eraseDigit">
                <el-icon><Backspace24Regular /></el-icon>
              </button>
            </div>
            <small v-if="callee && !CALLEE_PATTERN.test(callee)">号码只能包含可选的前导 + 和 1–32 位数字</small>
          </div>

          <PhoneDialPad @digit="appendDigit" />

          <div class="call-mode-actions" aria-label="呼叫模式">
            <button
              type="button"
              class="dial-button is-listen"
              :disabled="!canPlaceCall || !!action"
              title="只接收对方声音，不申请麦克风"
              @click="startListenOnlyCall"
            >
              <el-icon><Speaker224Regular /></el-icon>{{ action === '仅听呼叫' ? '正在呼叫…' : '仅听呼叫' }}
            </button>
            <button
              type="button"
              class="dial-button"
              :disabled="!canPlaceCall || !!action || !phone.secureContext"
              title="需要受信任的 HTTPS 和麦克风权限"
              @click="startTwoWayCall"
            >
              <el-icon><Call24Regular /></el-icon>{{ action === '双向呼叫' ? '正在呼叫…' : '双向呼叫' }}
            </button>
          </div>
          <p class="call-mode-help">
            {{ phone.secureContext
              ? '仅听呼叫不会使用麦克风；双向呼叫会请求麦克风权限。'
              : '当前页面只能仅听呼叫，对方听不到你。' }}
          </p>
        </div>
      </section>

        <PhoneCallHistory :records="phone.history" />
      </div>
    </section>
  </div>
</template>

<style scoped src="../styles/phone.css"></style>
