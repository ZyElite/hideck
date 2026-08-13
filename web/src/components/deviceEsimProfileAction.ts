export type EsimProfileAction = 'disable' | 'switch'

export function esimProfileActionForState(state: number): EsimProfileAction {
  return state === 1 ? 'disable' : 'switch'
}
