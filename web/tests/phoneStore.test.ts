import assert from 'node:assert/strict'
import test from 'node:test'
import { createPinia, setActivePinia } from 'pinia'
import type { PhoneCall, PhoneEvent } from '../src/services/phone'
import { usePhoneStore } from '../src/stores/phone'
import { normalizeCallOwnership, phoneErrorMessage } from '../src/utils/phone'

function call(mediaId: string, overrides: Partial<PhoneCall> = {}): PhoneCall {
  return {
    call_id: 'call-1',
    device_id: 'wwan1',
    direction: 'outbound',
    peer: '888',
    status: 'connected',
    media_id: mediaId,
    started_at: '2026-08-13T12:00:00Z',
    read_only: false,
    ...overrides
  }
}

function event(id: number, value: PhoneCall): PhoneEvent {
  return { id, type: 'call_connected', call: value, time: '2026-08-13T12:00:00Z' }
}

test('normalizes broadcast ownership against the tab media session', () => {
  assert.equal(normalizeCallOwnership(call('other'), 'ours').read_only, true)
  assert.equal(normalizeCallOwnership(call('ours', { read_only: true }), 'ours').read_only, false)
  assert.equal(normalizeCallOwnership(call('', { read_only: false }), 'ours').read_only, false)
})

test('store ignores replayed events and never grants another media session control', () => {
  setActivePinia(createPinia())
  const store = usePhoneStore()
  store.mediaId = 'ours'
  store.handleEvent(event(10, call('other')))
  assert.equal(store.calls[0].read_only, true)
  store.handleEvent(event(10, call('ours')))
  assert.equal(store.calls[0].media_id, 'other')
  store.handleEvent(event(11, call('ours')))
  assert.equal(store.calls[0].read_only, false)
})

test('surfaces API error messages without hiding the underlying failure', () => {
  const error = { response: { data: { message: 'phone: media session is unavailable' } } }
  assert.equal(phoneErrorMessage(error, 'fallback'), 'phone: media session is unavailable')
  assert.equal(phoneErrorMessage(new Error('network down'), 'fallback'), 'network down')
})
