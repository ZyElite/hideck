import assert from 'node:assert/strict'
import test from 'node:test'
import type { BalanceQuery, CommandEvent } from '../src/types/commands'
import {
  balanceResultText,
  balanceTransportLabel,
  presentBalanceState,
  presentCommandEvent
} from '../src/utils/commandPresentation'

function commandEvent(overrides: Partial<CommandEvent>): CommandEvent {
  return {
    id: 1,
    execution_id: 'exec-1',
    kind: 'result',
    text: '',
    created_at: '2026-08-13T12:00:00Z',
    ...overrides
  }
}

function balanceQuery(overrides: Partial<BalanceQuery>): BalanceQuery {
  return {
    id: 'balance-1',
    device_id: 'wwan0',
    iccid: '',
    rule_id: 'rule-1',
    transport: 'sms',
    state: 'sending',
    parse_state: 'pending',
    started_at: '2026-08-13T12:00:00Z',
    expires_at: '2026-08-13T12:01:00Z',
    created_at: '2026-08-13T12:00:00Z',
    updated_at: '2026-08-13T12:00:00Z',
    ...overrides
  }
}

test('maps command events without turning running or failed work into success', () => {
  const accepted = commandEvent({ kind: 'accepted', execution: {
    id: 'exec-1', input: '/signal wwan0', command: 'signal', state: 'failed',
    created_at: '', updated_at: ''
  } })
  assert.deepEqual(presentCommandEvent(accepted), {
    title: '已发送', detail: '/signal wwan0', tone: 'sent'
  })
  assert.equal(presentCommandEvent(commandEvent({
    kind: 'progress',
    text: '正在读取信号',
    execution: { id: 'exec-1', input: '', command: 'signal', state: 'failed', created_at: '', updated_at: '' }
  })).tone, 'running')
  assert.deepEqual(presentCommandEvent(commandEvent({ kind: 'error', text: '设备离线' })), {
    title: '执行失败', detail: '设备离线', tone: 'danger'
  })
  assert.equal(presentCommandEvent(commandEvent({ kind: 'result', text: '查询完成' })).tone, 'success')
})

test('keeps all balance lifecycle states explicit', () => {
  assert.deepEqual(presentBalanceState(balanceQuery({ state: 'sending' })), {
    label: '正在发送', tone: 'running'
  })
  assert.equal(presentBalanceState(balanceQuery({ state: 'awaiting_reply' })).label, '等待回复')
  assert.equal(presentBalanceState(balanceQuery({ state: 'completed', parse_state: 'parsed' })).tone, 'parsed')
  assert.equal(presentBalanceState(balanceQuery({ state: 'completed', parse_state: 'unparsed' })).tone, 'success')
  assert.equal(presentBalanceState(balanceQuery({ state: 'timed_out' })).tone, 'danger')
  assert.equal(presentBalanceState(balanceQuery({ state: 'failed' })).label, '查询失败')
  assert.deepEqual(presentBalanceState(balanceQuery({ transport: 'manual', parse_state: 'manual', state: 'completed' })), {
    label: '手动设置', tone: 'manual'
  })
})

test('renders only values and transport facts returned by the backend', () => {
  assert.equal(balanceResultText(balanceQuery({ amount: '12.89', currency: 'GBP' })), '12.89 GBP')
  assert.equal(balanceResultText(balanceQuery({ summary: '剩余 300MB' })), '剩余 300MB')
  assert.equal(balanceResultText(balanceQuery({ state: 'failed', error: '未匹配规则' })), '未匹配规则')
  assert.equal(balanceTransportLabel(balanceQuery({ transport: 'sms' })), 'SMS')
  assert.equal(balanceTransportLabel(balanceQuery({ transport: 'ussd' })), 'USSD')
  assert.equal(balanceTransportLabel(balanceQuery({ transport: 'manual' })), '手动录入')
})
