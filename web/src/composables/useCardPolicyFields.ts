import { ref, watch, type Ref } from 'vue'
import type { CardPolicy } from '../types/api'

type EditablePolicyFields = Pick<CardPolicy, 'ip_version' | 'apn'>
type SaveResult = { ok: boolean; error?: { message?: string } }

export function useCardPolicyFields(
  source: Ref<CardPolicy | null>,
  save: (patch: Partial<EditablePolicyFields>) => Promise<SaveResult>,
  onChanged?: () => void
) {
  const ipVersion = ref<CardPolicy['ip_version']>('v4')
  const apn = ref('')
  const pending = ref<'ip_version' | 'apn' | null>(null)
  const error = ref('')
  const errorField = ref<'ip_version' | 'apn' | null>(null)

  watch(source, (policy) => {
    if (!policy || pending.value) return
    ipVersion.value = policy.ip_version || 'v4'
    apn.value = policy.apn || ''
  }, { immediate: true })

  async function persist(field: keyof EditablePolicyFields) {
    if (!source.value || pending.value) return
    const previous = source.value[field]
    if (field === 'apn') apn.value = apn.value.trim()
    const value = field === 'ip_version' ? ipVersion.value : apn.value
    if (value === previous) return

    pending.value = field
    error.value = ''
    errorField.value = null
    const result = await save({ [field]: value })
    pending.value = null
    if (!result.ok) {
      if (field === 'ip_version') ipVersion.value = source.value.ip_version
      else apn.value = source.value.apn
      error.value = result.error?.message || '策略保存失败'
      errorField.value = field
      return
    }
    onChanged?.()
  }

  return {
    ipVersion, apn, pending, error, errorField,
    saveIPVersion: () => persist('ip_version'), saveAPN: () => persist('apn')
  }
}
