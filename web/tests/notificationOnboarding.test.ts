import assert from 'node:assert/strict'
import test from 'node:test'
import {
  buildWeComBotSettings,
  buildWeixinSettings,
  splitIDs,
  weComBotFormFromSettings,
  weixinFormFromSettings
} from '../src/stores/notificationChannelForms'
import { notificationQRPresentation, shouldShowQRActivateHint } from '../src/utils/notificationQrPresentation'

test('presents every QR state with explicit text and tone', () => {
  assert.deepEqual(notificationQRPresentation(null, false), { label: '未连接', tone: 'neutral' })
  assert.deepEqual(notificationQRPresentation(null, true), { label: '已配置', tone: 'success' })
  assert.deepEqual(notificationQRPresentation({ session_id: '1', status: 'wait' }, false), { label: '等待扫码', tone: 'active' })
  assert.deepEqual(notificationQRPresentation({ session_id: '1', status: 'scaned' }, false), { label: '已扫码，等待确认', tone: 'active' })
  assert.deepEqual(notificationQRPresentation({ session_id: '1', status: 'expired' }, false), { label: '二维码已过期', tone: 'warning' })
  assert.deepEqual(notificationQRPresentation({ session_id: '1', status: 'error' }, false), { label: '连接失败', tone: 'danger' })
  assert.deepEqual(notificationQRPresentation({ session_id: '1', status: 'confirmed', applied: true }, false), { label: '已连接', tone: 'success' })
})

test('shows the first-chat activation hint only after QR is confirmed', () => {
  assert.equal(shouldShowQRActivateHint(null), false)
  assert.equal(shouldShowQRActivateHint({ session_id: '1', status: 'wait' }), false)
  assert.equal(shouldShowQRActivateHint({ session_id: '1', status: 'scaned' }), false)
  assert.equal(shouldShowQRActivateHint({ session_id: '1', status: 'confirmed' }), true)
  assert.equal(shouldShowQRActivateHint({ session_id: '1', status: 'confirmed', applied: true }), true)
})

test('normalizes notification binding IDs without duplicates', () => {
  assert.deepEqual(splitIDs(' user-1, user-1, group-2, '), ['user-1', 'group-2'])
})

test('maps personal Weixin settings between API arrays and editable fields', () => {
  const form = weixinFormFromSettings({
    enabled: true,
    base_url: 'https://ilink.example.com',
    allowed_user_ids: ['user-1'],
    allowed_group_ids: ['group-1']
  })
  assert.equal(form.allowed_user_ids, 'user-1')
  assert.deepEqual(buildWeixinSettings(form), {
    enabled: true,
    base_url: 'https://ilink.example.com',
    allowed_user_ids: ['user-1'],
    allowed_group_ids: ['group-1']
  })
})

test('preserves masked WeCom Bot secrets while mapping manual settings', () => {
  const form = weComBotFormFromSettings({
    enabled: true,
    bot_id: 'bot-1',
    secret: '********',
    websocket_url: 'wss://openws.work.weixin.qq.com',
    allowed_user_ids: ['user-1'],
    allowed_group_ids: []
  })
  assert.equal(form.secret, '********')
  assert.equal(buildWeComBotSettings(form).secret, '********')
})
