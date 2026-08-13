import assert from 'node:assert/strict'
import test from 'node:test'
import { createPinia, setActivePinia } from 'pinia'
import { upstreamProxyService } from '../src/services/upstream-proxy.ts'
import { useUpstreamProxyStore } from '../src/stores/upstream-proxy.ts'
import { fail, ok, type ServiceResult } from '../src/types/domain.ts'
import type {
  UpstreamProxy,
  UpstreamProxyProbeResponse
} from '../src/types/api.ts'

type ServiceSnapshot = Pick<
  typeof upstreamProxyService,
  'list' | 'listCountries' | 'listCountryRules' | 'probe'
>

function saveService(): ServiceSnapshot {
  return {
    list: upstreamProxyService.list,
    listCountries: upstreamProxyService.listCountries,
    listCountryRules: upstreamProxyService.listCountryRules,
    probe: upstreamProxyService.probe
  }
}

function restoreService(snapshot: ServiceSnapshot): void {
  upstreamProxyService.list = snapshot.list
  upstreamProxyService.listCountries = snapshot.listCountries
  upstreamProxyService.listCountryRules = snapshot.listCountryRules
  upstreamProxyService.probe = snapshot.probe
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

function proxy(id: string, enabled = true): UpstreamProxy {
  return { id, name: id, addr: '198.51.100.10:1080', username: '', enabled }
}

function healthyProbe(durationMs: number): ServiceResult<UpstreamProxyProbeResponse> {
  return ok({
    status: 'ok',
    message: '前置代理探测成功',
    result: {
      proxy_addr: '198.51.100.10:1080',
      stage: 'ok',
      reachable: true,
      handshake_ok: true,
      udp_associate_ok: true,
      duration_ms: durationMs
    }
  })
}

test('store isolates a failed row probe and skips disabled proxies', async () => {
  const original = saveService()
  const probeCalls: string[] = []
  upstreamProxyService.list = async () => ok([proxy('enabled'), proxy('disabled', false)])
  upstreamProxyService.listCountries = async () => ok([])
  upstreamProxyService.listCountryRules = async () => ok([])
  upstreamProxyService.probe = async (id) => {
    probeCalls.push(id)
    return fail({ message: 'UDP Associate 被拒绝', status: 502 })
  }

  try {
    setActivePinia(createPinia())
    const store = useUpstreamProxyStore()
    const result = await store.fetchAll()

    assert.equal(result.ok, true)
    assert.deepEqual(probeCalls, ['enabled'])
    assert.equal(store.error, null)
    assert.equal(store.probeStatusMap.enabled?.state, 'unhealthy')
    assert.equal(store.probeStatusMap.disabled?.state, 'disabled')
  } finally {
    restoreService(original)
  }
})

test('an older list response cannot overwrite a newer fetch run', async () => {
  const original = saveService()
  const oldList = deferred<ServiceResult<UpstreamProxy[]>>()
  const newList = deferred<ServiceResult<UpstreamProxy[]>>()
  const newHealth = deferred<ServiceResult<UpstreamProxyProbeResponse>>()
  const lists = [oldList, newList]
  const probeCalls: string[] = []
  upstreamProxyService.list = async () => lists.shift()!.promise
  upstreamProxyService.listCountries = async () => ok([])
  upstreamProxyService.listCountryRules = async () => ok([])
  upstreamProxyService.probe = async (id) => {
    probeCalls.push(id)
    return newHealth.promise
  }

  try {
    setActivePinia(createPinia())
    const store = useUpstreamProxyStore()
    const staleFetch = store.fetchAll()
    const currentFetch = store.fetchAll()

    newList.resolve(ok([proxy('new')]))
    await new Promise(resolve => setTimeout(resolve, 0))
    oldList.resolve(ok([proxy('old')]))
    await staleFetch

    assert.deepEqual(store.proxies.map(item => item.id), ['new'])
    assert.deepEqual(probeCalls, ['new'])
    assert.equal(store.loading, true)

    newHealth.resolve(healthyProbe(7))
    await currentFetch
    assert.equal(store.loading, false)
    assert.equal(store.probeStatusMap.new?.state, 'healthy')
    assert.equal(store.probeStatusMap.old, undefined)
  } finally {
    restoreService(original)
  }
})
