import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'
import { normalizeThread, resolveSmsThreadKey, smsService } from '../src/services/sms.ts'
import { api } from '../src/stores/auth.ts'

test('SMS contact normalization uses persistent server unread state and ICCID identity', () => {
  const thread = normalizeThread({
    imsi: 'imsi-1', iccid: 'iccid-1', peer: '+10086', last_sms_id: 8,
    last_timestamp: '2026-08-13T18:00:00Z', last_content: 'hello', last_type: 1,
    unread_count: 2
  })
  assert.equal(thread.key, 'iccid-1|+10086')
  assert.equal(thread.iccid, 'iccid-1')
  assert.equal(thread.unreadCount, 2)
})

test('legacy IMSI thread keys resolve only when the ICCID thread is unique', () => {
  const first = normalizeThread({
    imsi: 'shared-imsi', iccid: 'iccid-1', peer: '+10086', last_sms_id: 1,
    last_timestamp: '2026-08-13T18:00:00Z', last_content: 'first', last_type: 1,
    unread_count: 1
  })
  assert.equal(resolveSmsThreadKey('shared-imsi|+10086', [first]), first.key)
  assert.equal(resolveSmsThreadKey(first.key, [first]), first.key)

  const second = { ...first, key: 'iccid-2|+10086', iccid: 'iccid-2' }
  assert.equal(resolveSmsThreadKey('shared-imsi|+10086', [first, second]), '')
})

test('SMS view does not override persistent unread state with browser localStorage', () => {
  const source = fs.readFileSync(new URL('../src/views/Sms.vue', import.meta.url), 'utf8')
  assert.doesNotMatch(source, /sms_thread_last_seen|localStorage/)
  assert.match(source, /smsStore\.send\(\{ iccid: t\.iccid, phone: t\.peer, message: text \}\)/)
  assert.doesNotMatch(source, /smsStore\.send\(\{ imsi: t\.imsi/)
  assert.match(source, /const payload = \{ iccid: thread\.iccid, peer: thread\.peer \}/)
})

test('markThreadRead sends the displayed message boundary to the persistent API', async () => {
  const originalPatch = api.patch
  const calls: unknown[][] = []
  api.patch = (async (...args: unknown[]) => {
    calls.push(args)
    return { data: { marked: 2, unread_count: 1, through_id: 12 } }
  }) as typeof api.patch

  try {
    const result = await smsService.markThreadRead({ iccid: 'iccid-1', peer: '+10086', through_id: 12 })
    assert.equal(result.ok, true)
    assert.deepEqual(calls, [[
      '/sms/thread',
      { through_id: 12 },
      { params: { iccid: 'iccid-1', peer: '+10086' } }
    ]])
  } finally {
    api.patch = originalPatch
  }
})

test('deleteThread sends ICCID as the exact conversation identity', async () => {
  const originalDelete = api.delete
  const calls: unknown[][] = []
  api.delete = (async (...args: unknown[]) => {
    calls.push(args)
    return { data: { deleted: 2, iccid: 'iccid-2', peer: '+10086' } }
  }) as typeof api.delete

  try {
    const result = await smsService.deleteThread({ iccid: 'iccid-2', peer: '+10086' })
    assert.equal(result.ok, true)
    assert.deepEqual(calls, [[
      '/sms/thread',
      { params: { peer: '+10086', iccid: 'iccid-2' } }
    ]])
  } finally {
    api.delete = originalDelete
  }
})
