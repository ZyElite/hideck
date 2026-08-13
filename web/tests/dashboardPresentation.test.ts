import assert from 'node:assert/strict'
import test from 'node:test'
import type { DashboardDevice } from '../src/types/api.ts'
import {
  createDashboardDevicePresentation,
  formatDashboardNetworkType,
  formatDashboardSignal,
  hasDashboardSignal
} from '../src/utils/dashboardPresentation.ts'

function createDevice(overrides: Partial<DashboardDevice> = {}): DashboardDevice {
  return {
    id: 'modem-1',
    name: 'London gateway',
    healthy: true,
    operator: 'giffgaff',
    network_duplex: 'FDD',
    network_mode: 'LTE',
    signal_dbm: -78,
    public_ip: '198.51.100.12',
    public_ipv6: '2001:db8:1200:34::89',
    ...overrides
  }
}

test('derives VoWiFi facts and preserves all runtime stage states', () => {
  const device = createDevice({
    vowifi_active: true,
    vowifi_runtime: {
      sim_ready: true,
      access_ready: true,
      tunnel_ready: false,
      ims_ready: undefined,
      sms_ready: true
    }
  })

  const presentation = createDashboardDevicePresentation(device)

  assert.equal(presentation.connectionTitle, 'Wi-Fi Calling')
  assert.equal(presentation.connectionState, '已连接')
  assert.equal(presentation.connectionType, 'VoWiFi')
  assert.equal(presentation.ipv4, '198.51.100.12')
  assert.equal(presentation.ipv6, '2001:db8:1200:34::89')
  assert.deepEqual(presentation.stages.map(stage => stage.ready), [true, true, false, undefined, true])
  assert.equal(Object.isFrozen(presentation), true)
})

test('uses explicit missing-value copy without mutating API data', () => {
  const device = createDevice({
    name: '',
    operator: '',
    network_duplex: '',
    network_mode: '',
    signal_dbm: Number.NaN,
    public_ip: '',
    public_ipv6: undefined
  })
  const before = { ...device }

  const presentation = createDashboardDevicePresentation(device)

  assert.equal(presentation.displayName, 'modem-1')
  assert.equal(presentation.operator, '不可用')
  assert.equal(presentation.connectionType, '不可用')
  assert.equal(presentation.signal, '不可用')
  assert.equal(presentation.ipv4, '未分配')
  assert.equal(presentation.ipv6, '未分配')
  assert.deepEqual(device, before)
})

test('keeps offline state distinct from a failed or unknown VoWiFi stage', () => {
  const presentation = createDashboardDevicePresentation(createDevice({
    healthy: false,
    vowifi_active: true,
    vowifi_runtime: { sim_ready: false }
  }))

  assert.equal(presentation.statusLabel, '离线')
  assert.equal(presentation.connectionTitle, '设备离线')
  assert.equal(presentation.connectionState, '当前设备不可用')
  assert.deepEqual(presentation.stages.map(stage => stage.ready), [false, undefined, undefined, undefined, undefined])
})

test('formats cellular connection and validates signal sentinels', () => {
  assert.equal(formatDashboardNetworkType(createDevice()), 'FDD LTE')
  assert.equal(formatDashboardSignal(-105), '-105 dBm')
  assert.equal(formatDashboardSignal(0), '不可用')
  assert.equal(formatDashboardSignal(-999), '不可用')
  assert.equal(hasDashboardSignal(-78), true)
  assert.equal(hasDashboardSignal(Number.POSITIVE_INFINITY), false)
})
