import assert from 'node:assert/strict'
import test from 'node:test'
import { readPhoneControl, savePhoneControl } from '../src/services/phone-session'

test('keeps the control lease in tab-scoped session storage and clears empty control', (context) => {
  const values = new Map<string, string>()
  const fakeStorage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key)
  }
  Object.defineProperty(globalThis, 'sessionStorage', { value: fakeStorage, configurable: true })
  context.after(() => { delete (globalThis as { sessionStorage?: unknown }).sessionStorage })

  savePhoneControl({ mediaId: 'media-1', lease: 'lease-1' })
  assert.deepEqual(readPhoneControl(), { mediaId: 'media-1', lease: 'lease-1' })
  savePhoneControl({ mediaId: '', lease: '' })
  assert.deepEqual(readPhoneControl(), { mediaId: '', lease: '' })
  assert.equal(values.size, 0)
})

test('discards malformed saved control instead of manufacturing a lease', (context) => {
  const values = new Map([['vohive_phone_control', '{invalid']])
  const fakeStorage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key)
  }
  Object.defineProperty(globalThis, 'sessionStorage', { value: fakeStorage, configurable: true })
  context.after(() => { delete (globalThis as { sessionStorage?: unknown }).sessionStorage })

  assert.deepEqual(readPhoneControl(), { mediaId: '', lease: '' })
  assert.equal(values.size, 0)
})
