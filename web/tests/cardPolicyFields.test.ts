import assert from 'node:assert/strict'
import test from 'node:test'
import { nextTick, ref } from 'vue'
import { useCardPolicyFields } from '../src/composables/useCardPolicyFields'
import type { CardPolicy } from '../src/types/api'

function policy(): CardPolicy {
  return {
    iccid: '8986001',
    network_enabled: false,
    vowifi_enabled: false,
    airplane_enabled: false,
    ip_version: 'v4v6',
    apn: 'ims',
    source: 'user'
  }
}

test('clearing APN persists an explicit empty value', async () => {
  const source = ref<CardPolicy | null>(policy())
  const patches: Array<Record<string, unknown>> = []
  const fields = useCardPolicyFields(source, async (patch) => {
    patches.push(patch)
    return { ok: true }
  })
  await nextTick()

  fields.apn.value = '   '
  await fields.saveAPN()

  assert.deepEqual(patches, [{ apn: '' }])
  assert.equal(fields.apn.value, '')
  assert.equal(fields.error.value, '')
})

test('failed field save restores the persisted value and exposes the error', async () => {
  const source = ref<CardPolicy | null>(policy())
  const fields = useCardPolicyFields(source, async () => ({
    ok: false,
    error: { message: 'database unavailable' }
  }))
  await nextTick()

  fields.ipVersion.value = 'v6'
  await fields.saveIPVersion()

  assert.equal(fields.ipVersion.value, 'v4v6')
  assert.equal(fields.error.value, 'database unavailable')
  assert.equal(fields.errorField.value, 'ip_version')
  assert.equal(fields.pending.value, null)
})
