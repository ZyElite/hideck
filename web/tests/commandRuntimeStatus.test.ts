import assert from 'node:assert/strict'
import test from 'node:test'
import { useCommandRuntimeStatus } from '../src/composables/useCommandRuntimeStatus'
import { fail, ok } from '../src/types/domain'
import type { BalanceQuery } from '../src/types/commands'

const balance: BalanceQuery = {
  id: 'balance-1',
  device_id: 'wwan0',
  iccid: '',
  rule_id: 'rule-1',
  transport: 'sms',
  state: 'awaiting_reply',
  parse_state: 'pending',
  started_at: '2026-08-13T12:00:00Z',
  expires_at: '2026-08-13T12:01:00Z',
  created_at: '2026-08-13T12:00:00Z',
  updated_at: '2026-08-13T12:00:00Z'
}

test('command runtime refresh updates devices and balances as one successful snapshot', async () => {
  const runtime = useCommandRuntimeStatus({
    fetchDevices: async () => ok({ devices: [{
      id: 'wwan0', name: 'Modem', running: true, healthy: true,
      network_connected: false, public_ip: '', sms_enabled: true, network_enabled: false
    }] }),
    fetchBalances: async () => ok([balance]),
    now: () => 42
  })

  assert.equal(await runtime.refresh(), true)
  assert.equal(runtime.devices.value[0]?.id, 'wwan0')
  assert.equal(runtime.balances.value[0]?.state, 'awaiting_reply')
  assert.equal(runtime.lastSyncedAt.value, 42)
  assert.equal(runtime.syncWarning.value, '')
})

test('command runtime refresh keeps prior data and exposes each real failure until recovery', async () => {
  let failing = false
  const foregroundErrors: string[] = []
  const runtime = useCommandRuntimeStatus({
    fetchDevices: async () => failing ? fail({ message: '设备接口超时' }) : ok({ devices: [] }),
    fetchBalances: async () => failing ? fail({ message: '余额接口中断' }) : ok([balance]),
    onForegroundError: (message) => foregroundErrors.push(message),
    now: () => 99
  })

  await runtime.refresh()
  failing = true
  assert.equal(await runtime.refresh(), false)
  assert.equal(runtime.balances.value.length, 1)
  assert.equal(runtime.lastSyncedAt.value, 99)
  assert.equal(runtime.syncWarning.value, '设备状态：设备接口超时；余额状态：余额接口中断')
  assert.deepEqual(foregroundErrors, ['设备状态：设备接口超时；余额状态：余额接口中断'])

  failing = false
  assert.equal(await runtime.refresh({ background: true }), true)
  assert.equal(runtime.syncWarning.value, '')
})

test('command runtime refresh coalesces overlapping polling and manual requests', async () => {
  let releaseDevices: (() => void) | undefined
  let deviceRequests = 0
  let balanceRequests = 0
  const deviceGate = new Promise<void>((resolve) => { releaseDevices = resolve })
  const runtime = useCommandRuntimeStatus({
    fetchDevices: async () => {
      deviceRequests += 1
      await deviceGate
      return ok({ devices: [] })
    },
    fetchBalances: async () => {
      balanceRequests += 1
      return ok([])
    }
  })

  const pollingRefresh = runtime.refresh({ background: true })
  const manualRefresh = runtime.refresh()
  assert.equal(pollingRefresh, manualRefresh)
  assert.equal(deviceRequests, 1)
  assert.equal(balanceRequests, 1)

  releaseDevices?.()
  assert.equal(await manualRefresh, true)
})
