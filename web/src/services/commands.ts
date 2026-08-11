import { api } from '../stores/auth'
import type { BalanceQuery, CarrierQueryRule, CommandDefinition, CommandEvent, CommandExecution } from '../types/commands'
import { callService } from './http'

export const commandService = {
  catalog() {
    return callService(async () => {
      const response = await api.get('/commands/catalog')
      return (response.data?.commands || []) as CommandDefinition[]
    })
  },

  execute(input: string) {
    return callService(async () => {
      const response = await api.post('/commands/executions', { input })
      return response.data?.execution as CommandExecution
    })
  },

  events(params: { afterId?: number; beforeId?: number; limit?: number }) {
    return callService(async () => {
      const response = await api.get('/commands/events', {
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
      const response = await api.delete('/commands/history')
      return Number(response.data?.deleted || 0)
    })
  },

  balances(params: { deviceId?: string; limit?: number; before?: string } = {}) {
    return callService(async () => {
      const response = await api.get('/balance/queries', {
        params: { device_id: params.deviceId, limit: params.limit || 50, before: params.before }
      })
      return (response.data?.queries || []) as BalanceQuery[]
    })
  },

  startBalance(deviceId: string) {
    return callService(async () => {
      const response = await api.post('/balance/queries', { device_id: deviceId })
      return response.data?.query as BalanceQuery
    })
  },

  rules() {
    return callService(async () => {
      const response = await api.get('/balance/rules')
      return {
        builtIn: (response.data?.built_in || []) as CarrierQueryRule[],
        custom: (response.data?.custom || []) as CarrierQueryRule[]
      }
    })
  },

  saveRule(rule: CarrierQueryRule) {
    return callService(async () => {
      const response = await api.put(`/balance/rules/${encodeURIComponent(rule.id)}`, rule)
      return response.data?.rule as CarrierQueryRule
    })
  },

  deleteRule(id: string) {
    return callService(async () => {
      await api.delete(`/balance/rules/${encodeURIComponent(id)}`)
      return true
    })
  }
}
