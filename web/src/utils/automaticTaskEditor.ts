import type { DeviceMgmtListItem, EsimEUICCProfiles } from '../types/api'
import type { AutomaticTask, AutomaticTaskInput } from '../types/automation'

export type AutomaticTaskProfileOption = Readonly<{
  key: string
  iccid: string
  aid: string
  label: string
}>

export function defaultAutomaticTaskInput(
  now = new Date(),
  timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
): AutomaticTaskInput {
  const localDate = [now.getFullYear(), now.getMonth() + 1, now.getDate()]
    .map((value, index) => index === 0 ? String(value) : String(value).padStart(2, '0'))
    .join('-')
  return {
    name: '', enabled: true, device_id: '', profile_iccid: '', profile_aid: '',
    task_type: 'sms', environment: 'vowifi', interval_days: 1,
    start_date: localDate, run_time: '09:00', timezone,
    payload: { phone: '', message: '', hold_seconds: 10 }, retry_count: 0, notify: true
  }
}

export function automaticTaskToInput(task: Readonly<AutomaticTask>): AutomaticTaskInput {
  return {
    name: task.name, enabled: task.enabled, device_id: task.device_id,
    profile_iccid: task.profile_iccid, profile_aid: task.profile_aid,
    task_type: task.task_type, environment: task.environment,
    interval_days: task.interval_days, start_date: task.start_date,
    run_time: task.run_time, timezone: task.timezone,
    payload: { ...task.payload }, retry_count: task.retry_count, notify: task.notify
  }
}

export function automaticTaskProfileKey(iccid: string, aid: string): string {
  return `${iccid}|${aid}`
}

export function currentAutomaticTaskICCID(device?: Readonly<DeviceMgmtListItem>): string {
  return (device?.modem?.iccid || device?.vowifi_runtime?.iccid || '').trim()
}

export function automaticTaskProfileOption(iccid: string, aid: string, name: string): AutomaticTaskProfileOption {
  return { key: automaticTaskProfileKey(iccid, aid), iccid, aid, label: `${name} · ${iccid}` }
}

export function mergeAutomaticTaskProfiles(
  existing: readonly AutomaticTaskProfileOption[],
  added: readonly AutomaticTaskProfileOption[]
): AutomaticTaskProfileOption[] {
  const options = new Map(existing.map((option) => [option.key, option]))
  for (const option of added) options.set(option.key, option)
  return [...options.values()]
}

export function flattenAutomaticTaskProfiles(groups: readonly EsimEUICCProfiles[]): AutomaticTaskProfileOption[] {
  return groups.flatMap((group) => group.profiles.map((profile) => automaticTaskProfileOption(
    profile.iccid,
    group.aid_hex,
    profile.name || profile.service_provider_name || 'eSIM'
  )))
}

export function validateAutomaticTaskInput(form: Readonly<AutomaticTaskInput>): string {
  if (!form.name.trim()) return '请输入任务名称'
  if (!form.device_id) return '请选择设备'
  if (!form.profile_iccid) return '当前设备尚未读取到 SIM ICCID'
  if (!form.start_date || !form.run_time || !form.timezone.trim()) return '请填写完整执行时间与时区'
  if (form.task_type === 'sms' && (!form.payload.phone?.trim() || !form.payload.message?.trim())) return '请填写短信号码和内容'
  if (form.task_type === 'call' && !form.payload.phone?.trim()) return '请填写通话号码'
  return ''
}
