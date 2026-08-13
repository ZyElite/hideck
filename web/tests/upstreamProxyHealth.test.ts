import assert from 'node:assert/strict'
import test from 'node:test'
import { fail, ok, type ServiceResult } from '../src/types/domain.ts'
import type {
  UpstreamProxy,
  UpstreamProxyProbeResponse
} from '../src/types/api.ts'
import {
  createUpstreamProbeRunner,
  type UpstreamProxyHealthMap
} from '../src/utils/upstreamProxyHealth.ts'

const enabledProxy: UpstreamProxy = {
  id: 'route-1',
  name: 'Route 1',
  addr: '198.51.100.10:1080',
  username: '',
  enabled: true
}

function successfulProbe(durationMs: number): ServiceResult<UpstreamProxyProbeResponse> {
  return ok({
    status: 'ok',
    message: '前置代理探测成功',
    result: {
      proxy_addr: enabledProxy.addr,
      stage: 'ok',
      reachable: true,
      handshake_ok: true,
      udp_associate_ok: true,
      duration_ms: durationMs,
      diagnosis: '代理支持标准 SOCKS5 UDP Associate'
    }
  })
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

test('probe runner skips disabled proxies and exposes row failures', async () => {
  const snapshots: UpstreamProxyHealthMap[] = []
  const calls: string[] = []
  const runner = createUpstreamProbeRunner({
    probe: async (id) => {
      calls.push(id)
      return fail({ message: '代理明确拒绝了 UDP Associate' })
    },
    publish: snapshot => snapshots.push(snapshot)
  })

  await runner.run([
    enabledProxy,
    { ...enabledProxy, id: 'disabled', enabled: false }
  ])

  assert.deepEqual(calls, ['route-1'])
  assert.equal(snapshots[0]?.['route-1']?.state, 'checking')
  assert.equal(snapshots.at(-1)?.['route-1']?.state, 'unhealthy')
  assert.equal(snapshots.at(-1)?.['disabled']?.state, 'disabled')
  assert.equal(snapshots.at(-1)?.['route-1']?.detail, '代理明确拒绝了 UDP Associate')
})

test('a stale probe run cannot overwrite a newer result', async () => {
  const first = deferred<ServiceResult<UpstreamProxyProbeResponse>>()
  const second = deferred<ServiceResult<UpstreamProxyProbeResponse>>()
  const queue = [first, second]
  const snapshots: UpstreamProxyHealthMap[] = []
  const runner = createUpstreamProbeRunner({
    probe: async () => queue.shift()!.promise,
    publish: snapshot => snapshots.push(snapshot)
  })

  const staleRun = runner.run([enabledProxy])
  const currentRun = runner.run([enabledProxy])
  second.resolve(successfulProbe(12))
  await currentRun
  first.resolve(fail({ message: '旧请求超时' }))
  await staleRun

  assert.equal(snapshots.at(-1)?.['route-1']?.state, 'healthy')
  assert.equal(snapshots.at(-1)?.['route-1']?.durationMs, 12)
})
