import type { CarrierQueryRule } from '../types/commands'

export type CarrierRuleOperationState = Readonly<{
  loading: boolean
  saving: boolean
  deletingId: string
}>

export function isCarrierRuleOperationBlocked(state: CarrierRuleOperationState): boolean {
  return state.loading || state.saving || Boolean(state.deletingId)
}

export function editableCarrierRule(
  selected: CarrierQueryRule,
  customRules: readonly CarrierQueryRule[]
): CarrierQueryRule {
  const existingOverride = customRules.find((rule) => rule.id === selected.id)
  const source = existingOverride || selected
  return {
    ...source,
    expected_senders: [...(source.expected_senders || [])],
    limitations: [...(source.limitations || [])],
    built_in: false
  }
}
