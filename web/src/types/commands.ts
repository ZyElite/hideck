export type CommandDefinition = {
  name: string
  usage: string
  summary: string
  dangerous: boolean
  async: boolean
  device_argument: boolean
}

export type CommandExecution = {
  id: string
  input: string
  command: string
  state: 'running' | 'completed' | 'failed'
  error?: string
  started_at?: string
  completed_at?: string
  created_at: string
  updated_at: string
}

export type CommandAttachment = {
  type: 'audio'
  recording: string
  content_type: 'audio/mpeg'
}

export type CommandEvent = {
  id: number
  execution_id: string
  kind: 'accepted' | 'progress' | 'result' | 'error'
  text: string
  attachments?: CommandAttachment[]
  execution?: CommandExecution
  created_at: string
}

export type BalanceQuery = {
  id: string
  device_id: string
  iccid: string
  rule_id: string
  transport: 'sms' | 'ussd' | 'manual'
  state: 'sending' | 'awaiting_reply' | 'completed' | 'timed_out' | 'failed'
  parse_state: 'pending' | 'parsed' | 'unparsed' | 'manual'
  amount?: string
  currency?: string
  summary?: string
  raw_response?: string
  error?: string
  started_at: string
  expires_at: string
  completed_at?: string
  created_at: string
  updated_at: string
}

export type ManualBalanceInput = Readonly<{
  amount: string
  currency: string
}>

export type CarrierQueryRule = {
  id: string
  mcc: string
  mnc: string
  spn?: string
  variant?: string
  operator: string
  transport: 'sms' | 'ussd' | 'unsupported'
  destination?: string
  payload?: string
  response_mode: 'direct' | 'sms' | 'none'
  expected_senders?: string[]
  parser_pattern?: string
  currency?: string
  cost_status: string
  evidence_type: string
  evidence_url?: string
  limitations?: string[]
  alternative?: string
  enabled: boolean
  built_in?: boolean
}
