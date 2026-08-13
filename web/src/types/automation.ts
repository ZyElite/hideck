export type AutomaticTaskType = 'sms' | 'call' | 'public_ip'
export type AutomaticTaskEnvironment = 'vowifi' | 'cellular'
export type AutomaticTaskRunStatus = 'queued' | 'running' | 'success' | 'failed'

export type AutomaticTaskPayload = {
  phone?: string
  message?: string
  hold_seconds?: number
}

export type AutomaticTaskInput = {
  name: string
  enabled: boolean
  device_id: string
  profile_iccid: string
  profile_aid?: string
  task_type: AutomaticTaskType
  environment: AutomaticTaskEnvironment
  interval_days: number
  start_date: string
  run_time: string
  timezone: string
  payload: AutomaticTaskPayload
  retry_count: number
  notify: boolean
}

export type AutomaticTask = AutomaticTaskInput & {
  id: number
  next_run_at: string
  last_run_at?: string
  last_status?: AutomaticTaskRunStatus
  last_error?: string
  created_at: string
  updated_at: string
}

export type AutomaticTaskRun = {
  id: number
  task_id: number
  device_id: string
  scheduled_at: string
  started_at?: string
  finished_at?: string
  status: AutomaticTaskRunStatus
  attempts: number
  output?: string
  error?: string
  created_at: string
  updated_at: string
}
