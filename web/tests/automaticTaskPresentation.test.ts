import assert from 'node:assert/strict'
import test from 'node:test'
import type { AutomaticTask, AutomaticTaskRun } from '../src/types/automation'
import {
  automaticTaskEnvironmentLabel,
  automaticTaskNextRun,
  automaticTaskPayloadSummary,
  automaticTaskRunResult,
  automaticTaskScheduleLabel,
  automaticTaskStatus,
  automaticTaskSummary,
  automaticTaskTypeLabel,
  formatAutomaticTaskDate
} from '../src/utils/automaticTaskPresentation'

function task(overrides: Partial<AutomaticTask> = {}): AutomaticTask {
  return {
    id: 1,
    name: '余额查询',
    enabled: true,
    device_id: 'wwan0',
    profile_iccid: '8944110000000000000',
    profile_aid: '',
    task_type: 'sms',
    environment: 'vowifi',
    interval_days: 1,
    start_date: '2026-08-14',
    run_time: '09:00',
    timezone: 'Europe/London',
    payload: { phone: '43430', message: 'BAL' },
    retry_count: 1,
    notify: true,
    next_run_at: '2026-08-15T08:00:00Z',
    created_at: '2026-08-14T00:00:00Z',
    updated_at: '2026-08-14T00:00:00Z',
    ...overrides
  }
}

test('presents every automatic task state without success fallback', () => {
  assert.deepEqual(automaticTaskStatus(), { label: '未运行', tone: 'neutral' })
  assert.deepEqual(automaticTaskStatus('queued'), { label: '排队中', tone: 'info' })
  assert.deepEqual(automaticTaskStatus('running'), { label: '执行中', tone: 'warning' })
  assert.deepEqual(automaticTaskStatus('success'), { label: '成功', tone: 'success' })
  assert.deepEqual(automaticTaskStatus('failed'), { label: '失败', tone: 'danger' })
  assert.deepEqual(automaticTaskStatus('unexpected'), { label: '状态不可用', tone: 'neutral' })
})

test('derives type environment schedule and payload only from API fields', () => {
  const source = task()
  const snapshot = structuredClone(source)

  assert.equal(automaticTaskTypeLabel(source), '短信')
  assert.equal(automaticTaskEnvironmentLabel(source), 'VoWiFi')
  assert.equal(automaticTaskScheduleLabel(source), '每 1 天 09:00')
  assert.equal(automaticTaskPayloadSummary(source), 'BAL → 43430')
  assert.deepEqual(source, snapshot)

  assert.equal(automaticTaskTypeLabel(task({ task_type: 'call', payload: { phone: '888', hold_seconds: 15 } })), '通话 15 秒')
  assert.equal(automaticTaskPayloadSummary(task({ task_type: 'call', payload: {} })), '未提供呼叫号码')
  assert.equal(automaticTaskEnvironmentLabel(task({ environment: 'cellular' })), '蜂窝')
})

test('keeps missing or unknown API enumerations explicit', () => {
  const malformedType = task({ task_type: 'unexpected' as AutomaticTask['task_type'] })
  const malformedEnvironment = task({ environment: '' as AutomaticTask['environment'] })

  assert.equal(automaticTaskTypeLabel(malformedType), '任务类型不可用')
  assert.equal(automaticTaskPayloadSummary(malformedType), '任务内容不可用')
  assert.equal(automaticTaskEnvironmentLabel(malformedEnvironment), '运行环境不可用')
})

test('keeps disabled, missing and invalid scheduling facts explicit', () => {
  assert.equal(automaticTaskNextRun(task({ enabled: false }), () => '不应调用'), '已停用')
  assert.equal(automaticTaskNextRun(task({ next_run_at: '' })), '未安排')
  assert.equal(automaticTaskScheduleLabel(task({ interval_days: 0, run_time: '' })), '间隔未提供 时间未提供')
  assert.equal(formatAutomaticTaskDate('invalid'), '时间不可用')
})

test('summarizes real task counts and earliest enabled run', () => {
  const tasks = [
    task({ id: 1, last_status: 'running', next_run_at: '2026-08-16T08:00:00Z' }),
    task({ id: 2, next_run_at: '2026-08-15T08:00:00Z' }),
    task({ id: 3, enabled: false, last_status: 'running', next_run_at: '2026-08-14T08:00:00Z' })
  ]
  assert.deepEqual(automaticTaskSummary(tasks), {
    total: 3,
    enabled: 2,
    running: 2,
    nextRunAt: '2026-08-15T08:00:00Z'
  })
})

test('run result exposes real error or output and names missing data', () => {
  const run = { error: '连接超时', output: 'ignored' } as AutomaticTaskRun
  assert.equal(automaticTaskRunResult(run), '连接超时')
  assert.equal(automaticTaskRunResult({ output: '已发送' } as AutomaticTaskRun), '已发送')
  assert.equal(automaticTaskRunResult({} as AutomaticTaskRun), '未提供执行结果')
})
