export type CarrierRuleOperationState = Readonly<{
  loading: boolean
  saving: boolean
  deletingId: string
}>

export function isCarrierRuleOperationBlocked(state: CarrierRuleOperationState): boolean {
  return state.loading || state.saving || Boolean(state.deletingId)
}
