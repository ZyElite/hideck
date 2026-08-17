import assert from 'node:assert/strict'
import test from 'node:test'
import { nextTick, ref } from 'vue'
import { useCardPolicyToggles, type PolicyMirror } from '../src/composables/useCardPolicyToggles'

function mirror(partial: Partial<PolicyMirror> = {}): PolicyMirror {
  return {
    network_enabled: false,
    vowifi_enabled: false,
    airplane_enabled: false,
    phone_mode: 'wifi',
    data_strategy: 'on_demand',
    ...partial
  }
}

test('changing call mode turns software phone on instead of only saving the preference', async () => {
  const source = ref<PolicyMirror | null>(mirror())
  let applied: PolicyMirror | null = null
  const toggles = useCardPolicyToggles(source, {
    applyNetwork: async () => ({ ok: true }),
    applyVoWiFi: async () => ({ ok: true }),
    applyAirplane: async () => ({ ok: true }),
    applyPhoneMode: async (next) => {
      applied = next
      return { ok: true }
    }
  })
  await nextTick()

  await toggles.onPhoneModeChange('wifi')

  assert.equal(applied?.vowifi_enabled, true)
  assert.equal(applied?.phone_mode, 'wifi')
  assert.equal(applied?.network_enabled, false)
  assert.equal(applied?.airplane_enabled, true)
  assert.equal(toggles.local.value.vowifi_enabled, true)
})

test('switching to cellular on_demand arms software phone without forcing data on', async () => {
  const source = ref<PolicyMirror | null>(mirror({ vowifi_enabled: true }))
  let applied: PolicyMirror | null = null
  const toggles = useCardPolicyToggles(source, {
    applyNetwork: async () => ({ ok: true }),
    applyVoWiFi: async () => ({ ok: true }),
    applyAirplane: async () => ({ ok: true }),
    applyPhoneMode: async (next) => {
      applied = next
      return { ok: true }
    }
  })
  await nextTick()

  await toggles.onPhoneModeChange('cellular')

  assert.equal(applied?.vowifi_enabled, true)
  assert.equal(applied?.phone_mode, 'cellular')
  assert.equal(applied?.network_enabled, false)
  assert.equal(applied?.airplane_enabled, true)
})

test('switching to cellular keeps airplane off when network is already on', async () => {
  const source = ref<PolicyMirror | null>(mirror({ network_enabled: true, vowifi_enabled: true }))
  let applied: PolicyMirror | null = null
  const toggles = useCardPolicyToggles(source, {
    applyNetwork: async () => ({ ok: true }),
    applyVoWiFi: async () => ({ ok: true }),
    applyAirplane: async () => ({ ok: true }),
    applyPhoneMode: async (next) => {
      applied = next
      return { ok: true }
    }
  })
  await nextTick()

  await toggles.onPhoneModeChange('cellular')

  assert.equal(applied?.network_enabled, true)
  assert.equal(applied?.airplane_enabled, false)
})
