export type StartupState = 'idle' | 'loading' | 'ready' | 'error'

interface StartupGateState {
  isAuthenticated: boolean
  deviceTimeState: StartupState
  disclaimerState: StartupState
}

interface DisclaimerGateState extends StartupGateState {
  accepted: boolean
}

export function canRenderShell(state: StartupGateState): boolean {
  if (!state.isAuthenticated) return true
  return state.deviceTimeState === 'ready' && state.disclaimerState === 'ready'
}

export function canShowDisclaimer(state: DisclaimerGateState): boolean {
  return state.isAuthenticated && !state.accepted && canRenderShell(state)
}
