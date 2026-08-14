import type {
  AutomaticTask,
  AutomaticTaskRun,
  AutomaticTaskRunStatus
} from '../types/automation'

export type AutomaticTaskTone = 'neutral' | 'info' | 'warning' | 'success' | 'danger'

export type AutomaticTaskStatusPresentation = Readonly<{
  label: string
  tone: AutomaticTaskTone
}>

const STATUS_PRESENTATION: Readonly<Record<AutomaticTaskRunStatus, AutomaticTaskStatusPresentation>> = {
  queued: { label: '排队中', tone: 'info' },
  running: { label: '执行中', tone: 'warning' },
  success: { label: '成功', tone: 'success' },
  failed: { label: '失败', tone: 'danger' }
}

export function automaticTaskStatus(status?: string): AutomaticTaskStatusPresentation {
  if (!status) return { label: '未运行', tone: 'neutral' }
  return STATUS_PRESENTATION[status as AutomaticTaskRunStatus]
    ?? { label: '状态不可用', tone: 'neutral' }
}

export function automaticTaskTypeLabel(task: Readonly<AutomaticTask>): string {
  if (task.task_type === 'sms') return '短信'
  if (task.task_type === 'public_ip') return '公网 IP'
  if (task.task_type === 'call') {
    const seconds = task.payload.hold_seconds
    return seconds ? `通话 ${seconds} 秒` : '通话'
  }
  return '任务类型不可用'
}

export function automaticTaskEnvironmentLabel(task: Readonly<AutomaticTask>): string {
  if (task.environment === 'vowifi') return 'VoWiFi'
  if (task.environment === 'cellular') return '蜂窝'
  return '运行环境不可用'
}

export function automaticTaskScheduleLabel(task: Readonly<AutomaticTask>): string {
  const interval = Number.isFinite(task.interval_days) && task.interval_days > 0
    ? `每 ${task.interval_days} 天`
    : '间隔未提供'
  return `${interval} ${task.run_time || '时间未提供'}`
}

export function automaticTaskPayloadSummary(task: Readonly<AutomaticTask>): string {
  const phone = task.payload.phone?.trim()
  const message = task.payload.message?.trim()
  if (task.task_type === 'call') return phone || '未提供呼叫号码'
  if (task.task_type === 'public_ip') return '读取蜂窝公网 IP'
  if (task.task_type !== 'sms') return '任务内容不可用'
  if (phone && message) return `${message} → ${phone}`
  return message || phone || '未提供短信内容'
}

export function automaticTaskNextRun(
  task: Readonly<AutomaticTask>,
  formatter: (value: string) => string = formatAutomaticTaskDate
): string {
  if (!task.enabled) return '已停用'
  return task.next_run_at ? formatter(task.next_run_at) : '未安排'
}

export function automaticTaskSummary(tasks: readonly AutomaticTask[]) {
  const enabled = tasks.filter((task) => task.enabled)
  const running = tasks.filter((task) => task.last_status === 'running')
  const nextTask = enabled
    .filter((task) => validTimestamp(task.next_run_at))
    .sort((left, right) => Date.parse(left.next_run_at) - Date.parse(right.next_run_at))[0]
  return Object.freeze({
    total: tasks.length,
    enabled: enabled.length,
    running: running.length,
    nextRunAt: nextTask?.next_run_at || ''
  })
}

export function automaticTaskRunResult(run: Readonly<AutomaticTaskRun>): string {
  return run.error?.trim() || run.output?.trim() || '未提供执行结果'
}

export function formatAutomaticTaskDate(value?: string): string {
  if (!value) return '未提供'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '时间不可用'
  return date.toLocaleString()
}

function validTimestamp(value?: string): boolean {
  return Boolean(value) && !Number.isNaN(Date.parse(value || ''))
}
