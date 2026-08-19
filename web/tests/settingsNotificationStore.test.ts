import assert from 'node:assert/strict'
import test from 'node:test'
import { createPinia, setActivePinia } from 'pinia'
import {
  systemService,
  type NotificationsSettingsResponse
} from '../src/services/system.ts'
import { useSettingsStore } from '../src/stores/settings.ts'
import { ok, type ServiceResult } from '../src/types/domain.ts'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

test('a full notification refresh does not reuse an older binding snapshot', async () => {
  const originalGetNotifications = systemService.getNotifications
  const stale = deferred<ServiceResult<NotificationsSettingsResponse>>()
  const current = deferred<ServiceResult<NotificationsSettingsResponse>>()
  const requests = [stale, current]
  let requestCount = 0
  systemService.getNotifications = async () => {
    requestCount += 1
    return requests.shift()!.promise
  }

  try {
    setActivePinia(createPinia())
    const store = useSettingsStore()
    const staleBindingRefresh = store.refreshNotificationBinding('feishu')
    const currentFullRefresh = store.fetchNotifications({ silent: true })

    assert.equal(requestCount, 2)
    current.resolve(ok({
      feishu: { chat_ids: ['oc_current'] },
      qq: { enabled: true, app_id: 'qq_current', direct_ids: 'user_current' }
    }))
    await currentFullRefresh

    stale.resolve(ok({
      feishu: { chat_ids: ['oc_stale'] },
      qq: { enabled: false, app_id: 'qq_stale', direct_ids: '' }
    }))
    await staleBindingRefresh

    assert.equal(store.qqForm.enabled, true)
    assert.equal(store.qqForm.app_id, 'qq_current')
    assert.equal(store.qqForm.direct_ids, 'user_current')
  } finally {
    systemService.getNotifications = originalGetNotifications
  }
})
