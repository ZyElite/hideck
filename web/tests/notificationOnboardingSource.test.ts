import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

function source(relativePath: string): string {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8')
}

const settings = source('../src/views/Settings.vue')
const qrPanel = source('../src/components/settings/NotificationQrConnect.vue')
const weixinPanel = source('../src/components/settings/WeixinNotificationTab.vue')
const wecomPanel = source('../src/components/settings/WeComBotNotificationTab.vue')
const feishuPanel = source('../src/components/settings/FeishuNotificationTab.vue')
const telegramPanel = source('../src/components/settings/TelegramNotificationTab.vue')
const settingsStore = source('../src/stores/settings.ts')
const systemService = source('../src/services/system.ts')
const polling = source('../src/composables/useNotificationQR.ts')
const bindingPolling = source('../src/composables/useNotificationBindingPoll.ts')

test('settings separates personal Weixin, WeCom Bot, WeCom Webhook, and QQ', () => {
  assert.match(settings, /label="个人微信"/)
  assert.match(settings, /aria-label="个人微信（会话型通知渠道）"/)
  assert.match(settings, /label="企微机器人"/)
  assert.match(settings, /label="企微 Webhook"/)
  assert.match(settings, /label="QQ Bot"/)
})

test('Telegram uses token onboarding with an explicit private-chat binding state', () => {
  assert.match(settings, /<TelegramNotificationTab/)
  assert.match(telegramPanel, /@BotFather/)
  assert.match(telegramPanel, /请打开 Telegram，给这个 Bot 发送任意一条消息完成激活/)
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

test('WeCom QR reminds the user to message the bot after scan', () => {
  assert.match(weixinPanel, /activate-hint="扫码后，记得在微信里给机器人发句话/)
  assert.match(weixinPanel, /会话型通知渠道/)
  assert.match(weixinPanel, /太久没互动时，微信可能会暂停推送/)
  assert.match(weixinPanel, /通知会发给/)
  assert.match(weixinPanel, /useNotificationBindingPoll/)
  assert.match(weixinPanel, /refreshNotificationBinding\('weixin'\)/)
  assert.doesNotMatch(weixinPanel, /fetchNotifications\(\{ silent: true \}\)/)
  assert.match(weixinPanel, /RefreshButton/)
  assert.match(wecomPanel, /activate-hint="机器人已接入。请打开企业微信，给这个机器人发一条任意消息完成激活/)
  assert.match(wecomPanel, /useNotificationBindingPoll/)
  assert.match(wecomPanel, /refreshNotificationBinding\('wecom-bot'\)/)
  assert.match(wecomPanel, /RefreshButton/)
  assert.match(feishuPanel, /useNotificationQR\('feishu'/)
  assert.match(feishuPanel, /请用扫码的那个飞书账号给这个机器人发一条消息/)
  assert.match(feishuPanel, /useNotificationBindingPoll/)
  assert.match(feishuPanel, /refreshNotificationBinding\('feishu'\)/)
  assert.match(feishuPanel, /RefreshButton/)
  assert.match(qrPanel, /showActivateHint/)
  assert.match(qrPanel, /role="status"/)
  assert.match(qrPanel, /ElMessage\.warning/)
  assert.match(qrPanel, /shouldShowQRActivateHint\(props\.session, props\.connected\)/)
})

test('binding polling coalesces snapshots while full refresh starts a fresh request', () => {
  assert.match(settingsStore, /notificationSnapshotRequest/)
  assert.match(settingsStore, /notificationBindingHandlers\[channel\]\[mode\]/)
  assert.match(settingsStore, /mergeIDs\(feishuForm\.value\.chat_ids/)
  assert.doesNotMatch(bindingPolling, /fetchNotifications/)
  assert.match(
    settingsStore,
    /async function fetchNotifications[\s\S]*?const result = await getNotificationSnapshot\(true\)/
  )
  assert.match(settingsStore, /getNotificationSnapshot\(mode === 'replace'\)/)
  assert.equal(settingsStore.match(/systemService\.getNotifications\(\)/g)?.length, 2)
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
