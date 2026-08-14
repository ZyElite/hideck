import assert from 'node:assert/strict'
import test from 'node:test'
import type { AutomaticTask } from '../src/types/automation'
import {
  automaticTaskProfileKey,
  automaticTaskProfileOption,
  automaticTaskToInput,
  defaultAutomaticTaskInput,
  mergeAutomaticTaskProfiles,
  validateAutomaticTaskInput
} from '../src/utils/automaticTaskEditor'

test('automatic task editor defaults are deterministic for a supplied date and timezone', () => {
  const input = defaultAutomaticTaskInput(new Date(2026, 7, 4), 'Asia/Shanghai')
  assert.equal(input.start_date, '2026-08-04')
  assert.equal(input.timezone, 'Asia/Shanghai')
  assert.equal(input.task_type, 'sms')
  assert.equal(input.environment, 'vowifi')
})

test('automatic task editor copies payload instead of mutating the selected task', () => {
  const source = {
    name: '查询', enabled: true, device_id: 'wwan0', profile_iccid: '8944', profile_aid: '',
    task_type: 'sms', environment: 'vowifi', interval_days: 1, start_date: '2026-08-04',
    run_time: '09:00', timezone: 'UTC', payload: { phone: '100', message: 'BAL' }, retry_count: 0,
    notify: true
  } as AutomaticTask
  const copy = automaticTaskToInput(source)
  copy.payload.phone = '200'
  assert.equal(source.payload.phone, '100')
})

test('profile options preserve the exact ICCID and AID identity', () => {
  const first = automaticTaskProfileOption('8944', 'A0001', 'eSIM')
  const replacement = automaticTaskProfileOption('8944', 'A0001', '更新')
  assert.equal(automaticTaskProfileKey('8944', 'A0001'), '8944|A0001')
  assert.deepEqual(mergeAutomaticTaskProfiles([first], [replacement]), [replacement])
})

test('automatic task validation keeps required production fields explicit', () => {
  const input = defaultAutomaticTaskInput(new Date(2026, 7, 4), 'UTC')
  assert.equal(validateAutomaticTaskInput(input), '请输入任务名称')
  Object.assign(input, { name: '任务', device_id: 'wwan0', profile_iccid: '8944' })
  assert.equal(validateAutomaticTaskInput(input), '请填写短信号码和内容')
  Object.assign(input.payload, { phone: '100', message: 'BAL' })
  assert.equal(validateAutomaticTaskInput(input), '')
})
