import assert from 'node:assert/strict'
import test from 'node:test'
import { upstreamProxyService } from '../src/services/upstream-proxy.ts'
import { api } from '../src/stores/auth.ts'
import type { UpstreamProxyProbeResponse } from '../src/types/api.ts'

test('probe service posts to the encoded action path without a request body', async () => {
  const originalPost = api.post
  const calls: unknown[][] = []
  const response: UpstreamProxyProbeResponse = {
    status: 'ok',
    message: '前置代理探测成功',
    result: {
      proxy_addr: '198.51.100.10:1080',
      stage: 'ok',
      reachable: true,
      handshake_ok: true,
      udp_associate_ok: true,
      duration_ms: 4
    }
  }
  api.post = (async (...args: unknown[]) => {
    calls.push(args)
    return { data: response }
  }) as typeof api.post

  try {
    const result = await upstreamProxyService.probe('uk / primary')

    assert.equal(result.ok, true)
    assert.deepEqual(calls, [[
      '/upstream-proxies/uk%20%2F%20primary/actions/probe'
    ]])
  } finally {
    api.post = originalPost
  }
})
