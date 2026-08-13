import type { BalanceQuery, CommandEvent } from '../types/commands'

export type CommandEventTone = 'sent' | 'running' | 'success' | 'danger'
export type BalanceTone = 'running' | 'waiting' | 'parsed' | 'manual' | 'success' | 'danger'

export type CommandEventPresentation = Readonly<{
  title: string
  detail: string
  tone: CommandEventTone
}>

export type BalanceStatePresentation = Readonly<{
  label: string
  tone: BalanceTone
}>

export function presentCommandEvent(event: CommandEvent): CommandEventPresentation {
  if (event.kind === 'accepted') {
    return {
      title: '已发送',
      detail: event.execution?.input || event.text || '命令已提交',
      tone: 'sent'
    }
  }
  if (event.kind === 'progress' || event.execution?.state === 'running') {
    return { title: '执行中', detail: event.text || '命令正在执行', tone: 'running' }
  }
  if (event.kind === 'error' || event.execution?.state === 'failed') {
    return {
      title: '执行失败',
      detail: event.text || event.execution?.error || '命令执行失败',
      tone: 'danger'
    }
  }
  return { title: '执行成功', detail: event.text || '命令执行完成', tone: 'success' }
}

export function presentBalanceState(query: BalanceQuery): BalanceStatePresentation {
  if (query.transport === 'manual') return { label: '手动设置', tone: 'manual' }
  if (query.state === 'failed') return { label: '查询失败', tone: 'danger' }
  if (query.state === 'timed_out') return { label: '等待超时', tone: 'danger' }
  if (query.state === 'awaiting_reply') return { label: '等待回复', tone: 'waiting' }
  if (query.state === 'sending') return { label: '正在发送', tone: 'running' }
  if (query.parse_state === 'parsed') return { label: '已解析', tone: 'parsed' }
  return { label: '已收到', tone: 'success' }
}

export function balanceResultText(query: BalanceQuery): string {
  if (query.amount) return [query.amount, query.currency].filter(Boolean).join(' ')
  if (query.summary) return query.summary
  if (query.state === 'completed') return '已收到运营商回复'
  if (query.state === 'failed') return query.error || '余额查询失败'
  if (query.state === 'timed_out') return query.error || '未在有效期内收到回复'
  return '等待运营商回复'
}

export function balanceTransportLabel(query: BalanceQuery): string {
  if (query.transport === 'manual') return '手动录入'
  return query.transport === 'ussd' ? 'USSD' : 'SMS'
}
