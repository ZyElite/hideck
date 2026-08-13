import assert from 'node:assert/strict'
import test from 'node:test'
import type { DeviceMgmtListItem } from '../src/types/api'
import {
  createSmsConversationContext,
  createSmsDeviceChannels,
  normalizeSmsUnreadCount,
  smsPeerInitial,
  smsUnreadBadge
} from '../src/utils/smsPresentation'

function device(overrides: Partial<DeviceMgmtListItem> = {}): DeviceMgmtListItem {
  return {
    id: 'wwan0',
    name: 'EC25-01',
    running: true,
    healthy: true,
    control_online: true,
    network_connected: true,
    public_ip: '',
    sms_enabled: true,
    network_enabled: true,
    ...overrides
  }
}

test('presents device channels with explicit online text and real operator', () => {
  const rows = createSmsDeviceChannels([
    device({ modem: { operator: 'giffgaff' } }),
    device({ id: 'wwan1', name: 'EC25-02', control_online: false })
  ])

  assert.equal(rows[0].statusLabel, '1/2 在线')
  assert.equal(rows[1].detail, '在线 · giffgaff')
  assert.equal(rows[1].accessibilityLabel, 'EC25-01，在线，giffgaff')
  assert.equal(rows[2].statusLabel, '离线')
})

test('conversation context exposes only backend readiness facts', () => {
  const readyDevice = device({
    modem: { operator: 'giffgaff' },
    vowifi_runtime: { sms_ready: true, ims_ready: true }
  })
  const ready = createSmsConversationContext({
    selectedDeviceId: 'all',
    thread: { deviceId: 'wwan0', lastDeviceName: 'fallback' },
    devices: [readyDevice]
  })
  const unknown = createSmsConversationContext({ selectedDeviceId: 'all', thread: null, devices: [] })

  assert.deepEqual(ready, {
    deviceLabel: 'EC25-01',
    operatorLabel: 'giffgaff',
    smsLabel: 'VoWiFi · SMS 已就绪',
    smsTone: 'success',
    imsLabel: 'IMS 已注册',
    imsTone: 'success'
  })
  assert.equal(unknown.operatorLabel, '运营商未提供')
  assert.equal(unknown.smsLabel, 'SMS 状态未提供')
  assert.equal(unknown.imsLabel, 'IMS 状态未提供')
})

test('unread badges and initials are explicit and bounded', () => {
  assert.equal(smsUnreadBadge(3), 3)
  assert.equal(smsUnreadBadge(-2), 0)
  assert.equal(smsUnreadBadge(3, true), 0)
  assert.equal(smsPeerInitial(' giffgaff '), 'G')
  assert.equal(smsPeerInitial(''), '—')
})

test('normalizes backend unread counts without inventing negative values', () => {
  assert.equal(normalizeSmsUnreadCount(7), 7)
  assert.equal(normalizeSmsUnreadCount(-3), 0)
  assert.equal(normalizeSmsUnreadCount(undefined), 0)
})
