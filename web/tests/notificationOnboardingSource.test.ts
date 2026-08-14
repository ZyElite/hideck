import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

function source(relativePath: string): string {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8')
}

const settings = source('../src/views/Settings.vue')
const qrPanel = source('../src/components/settings/NotificationQrConnect.vue')
const polling = source('../src/composables/useNotificationQR.ts')

test('settings separates personal Weixin, WeCom Bot, WeCom Webhook, and QQ', () => {
  assert.match(settings, /label="个人微信"/)
  assert.match(settings, /label="企微机器人"/)
  assert.match(settings, /label="企微 Webhook"/)
  assert.match(settings, /label="QQ Bot"/)
})

test('QR panel renders a stable code and accessible status and actions', () => {
  assert.match(qrPanel, /QrcodeVue/)
  assert.match(qrPanel, /:size="184"/)
  assert.match(qrPanel, /aria-live="polite"/)
  assert.match(qrPanel, /aria-label="取消扫码"/)
  assert.match(qrPanel, /min-height: 216px/)
})

test('QR polling cancels timers and active sessions on unmount', () => {
  assert.match(polling, /onBeforeUnmount/)
  assert.match(polling, /clearTimer\(\)/)
  assert.match(polling, /notificationOnboardingService\.cancel/)
})
