import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

function source(relativePath: string): string {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8')
}

const settings = source('../src/views/Settings.vue')
const qrPanel = source('../src/components/settings/NotificationQrConnect.vue')
const telegramPanel = source('../src/components/settings/TelegramNotificationTab.vue')
const settingsStore = source('../src/stores/settings.ts')
const systemService = source('../src/services/system.ts')
const polling = source('../src/composables/useNotificationQR.ts')

test('settings separates personal Weixin, WeCom Bot, WeCom Webhook, and QQ', () => {
  assert.match(settings, /label="个人微信"/)
  assert.match(settings, /label="企微机器人"/)
  assert.match(settings, /label="企微 Webhook"/)
  assert.match(settings, /label="QQ Bot"/)
})

test('Telegram uses token onboarding with an explicit private-chat binding state', () => {
  assert.match(settings, /<TelegramNotificationTab/)
  assert.match(telegramPanel, /@BotFather/)
  assert.match(telegramPanel, /等待管理员首次私聊绑定/)
  assert.match(telegramPanel, /通知 Chat ID（可选）/)
  assert.match(telegramPanel, /grid-cols-1 gap-4 sm:grid-cols-2/)
  assert.doesNotMatch(telegramPanel, /NotificationQrConnect|二维码/)
})

test('Telegram recording delivery defaults to a voice bubble and remains configurable', () => {
  assert.match(telegramPanel, /录音发送样式/)
  assert.match(telegramPanel, /label: '语音气泡', value: 'voice'/)
  assert.match(telegramPanel, /label: '音频附件', value: 'audio'/)
  assert.match(telegramPanel, /v-model="telegramForm\.recording_mode"/)
  assert.match(settingsStore, /recording_mode: 'voice'/)
  assert.match(settingsStore, /recording_mode: telegramForm\.value\.recording_mode/)
  assert.match(systemService, /recording_mode: TelegramRecordingMode/)
})

test('QR panel renders a stable code and accessible status and actions', () => {
  assert.match(qrPanel, /QrcodeVue/)
  assert.match(qrPanel, /:size="184"/)
  assert.match(qrPanel, /aria-live="polite"/)
  assert.match(qrPanel, /aria-label="取消扫码"/)
  assert.match(qrPanel, /min-height: 216px/)
  assert.match(qrPanel, /:class="\{ 'is-visible': polling \}"/)
  assert.match(qrPanel, /min-width: 4em/)
  assert.match(qrPanel, /visibility: hidden/)
  assert.doesNotMatch(qrPanel, /v-if="polling"/)
})

test('QR polling cancels timers and active sessions on unmount', () => {
  assert.match(polling, /onBeforeUnmount/)
  assert.match(polling, /clearTimer\(\)/)
  assert.match(polling, /notificationOnboardingService\.cancel/)
})
