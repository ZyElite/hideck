import { api } from '../stores/auth'
import type { BalanceQuery, CarrierQueryRule, CommandDefinition, CommandEvent, CommandExecution } from '../types/commands'
import { callService } from './http'

export const commandService = {
  catalog() {
    return callService(async () => {
      const response = await api.get('/command-center/commands')
      return (response.data?.commands || []) as CommandDefinition[]
    })
  },

  execute(input: string) {
    return callService(async () => {
      const response = await api.post('/command-center/executions', { input })
      return response.data?.execution as CommandExecution
    })
  },

  events(params: { afterId?: number; beforeId?: number; limit?: number }) {
    return callService(async () => {
      const response = await api.get('/command-center/events', {
        params: {
          after_id: params.afterId,
          before_id: params.beforeId,
          limit: params.limit || 100
        }
      })
      return (response.data?.events || []) as CommandEvent[]
    })
  },

  clearHistory() {
    return callService(async () => {
      const response = await api.delete('/command-center/history')
      return Number(response.data?.deleted || 0)
    })
  },

  recording(recording: string, signal?: AbortSignal) {
    return callService(async () => {
      const response = await api.get(`/command-center/recordings/${encodeURIComponent(recording)}`, {
        responseType: 'blob', signal
      })
      return response.data as Blob
    })
  },

  balances(params: { deviceId?: string; limit?: number; before?: string } = {}) {
    return callService(async () => {
      const response = await api.get('/balances', {
        params: { device_id: params.deviceId, limit: params.limit || 50, before: params.before }
      })
      return (response.data?.queries || []) as BalanceQuery[]
    })
  },

  startBalance(deviceId: string) {
    return callService(async () => {
      const response = await api.post(`/devices/${encodeURIComponent(deviceId)}/balance-queries`)
      return response.data?.query as BalanceQuery
    })
  },

  rules() {
    return callService(async () => {
      const response = await api.get('/carrier-query-rules')
      return {
        builtIn: (response.data?.built_in || []) as CarrierQueryRule[],
        custom: (response.data?.custom || []) as CarrierQueryRule[]
      }
    })
  },

  createRule(rule: CarrierQueryRule) {
    return callService(async () => {
      const response = await api.post('/carrier-query-rules', rule)
      return response.data?.rule as CarrierQueryRule
    })
  },

  updateRule(id: string, rule: CarrierQueryRule) {
    return callService(async () => {
      const response = await api.put(`/carrier-query-rules/${encodeURIComponent(id)}`, rule)
      return response.data?.rule as CarrierQueryRule
    })
  },

  deleteRule(id: string) {
    return callService(async () => {
      await api.delete(`/carrier-query-rules/${encodeURIComponent(id)}`)
      return true
    })
  }
}
