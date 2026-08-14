import assert from 'node:assert/strict'
import test from 'node:test'
import { canRenderShell, canShowDisclaimer, type StartupState } from '../src/utils/startupGate'

const unsettledStates: StartupState[] = ['idle', 'loading', 'error']

test('disclaimer stays hidden until both authenticated startup checks are ready', () => {
  for (const deviceTimeState of unsettledStates) {
    assert.equal(canShowDisclaimer({
      isAuthenticated: true,
      deviceTimeState,
      disclaimerState: 'ready',
      accepted: false
    }), false)
  }
  for (const disclaimerState of unsettledStates) {
    assert.equal(canShowDisclaimer({
      isAuthenticated: true,
      deviceTimeState: 'ready',
      disclaimerState,
      accepted: false
    }), false)
  }
})

test('disclaimer appears only for an authenticated ready startup without acceptance', () => {
  const readyState = {
    isAuthenticated: true,
    deviceTimeState: 'ready' as const,
    disclaimerState: 'ready' as const
  }
  assert.equal(canShowDisclaimer({ ...readyState, accepted: false }), true)
  assert.equal(canShowDisclaimer({ ...readyState, accepted: true }), false)
  assert.equal(canShowDisclaimer({ ...readyState, isAuthenticated: false, accepted: false }), false)
})

test('authenticated shell remains blocked while either startup check is unsettled', () => {
  assert.equal(canRenderShell({
    isAuthenticated: true,
    deviceTimeState: 'error',
    disclaimerState: 'ready'
  }), false)
  assert.equal(canRenderShell({
    isAuthenticated: true,
    deviceTimeState: 'ready',
    disclaimerState: 'ready'
  }), true)
  assert.equal(canRenderShell({
    isAuthenticated: false,
    deviceTimeState: 'idle',
    disclaimerState: 'idle'
  }), true)
})
