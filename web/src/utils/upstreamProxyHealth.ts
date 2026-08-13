import type { ServiceResult } from '../types/domain'
import type {
  UpstreamProxy,
  UpstreamProxyProbeResponse,
  UpstreamProxyProbeResult
} from '../types/api'

export type UpstreamProxyHealthState = 'checking' | 'disabled' | 'healthy' | 'unhealthy'

export type UpstreamProxyHealth = Readonly<{
  state: UpstreamProxyHealthState
  detail: string
  durationMs?: number
}>

export type UpstreamProxyHealthMap = Readonly<Record<string, UpstreamProxyHealth>>

type ProbeRunnerConfig = Readonly<{
  probe: (id: string) => Promise<ServiceResult<UpstreamProxyProbeResponse>>
  publish: (snapshot: UpstreamProxyHealthMap) => void
}>

export function createUpstreamProbeRunner(config: ProbeRunnerConfig) {
  let activeRun = 0
  let snapshot: UpstreamProxyHealthMap = Object.freeze({})

  async function run(proxies: readonly UpstreamProxy[]): Promise<void> {
    const runId = ++activeRun
    snapshot = initialHealthMap(proxies)
    config.publish(snapshot)

    await Promise.all(proxies.filter(proxy => proxy.enabled).map(async (proxy) => {
      const result = await config.probe(proxy.id)
      if (runId !== activeRun) return
      snapshot = Object.freeze({ ...snapshot, [proxy.id]: healthFromProbe(result) })
      config.publish(snapshot)
    }))
  }

  function invalidate(): void {
    activeRun += 1
  }

  return Object.freeze({ invalidate, run })
}

function initialHealthMap(proxies: readonly UpstreamProxy[]): UpstreamProxyHealthMap {
  return Object.freeze(Object.fromEntries(proxies.map(proxy => [
    proxy.id,
    Object.freeze(proxy.enabled
      ? { state: 'checking', detail: '正在检测 SOCKS5 UDP Associate' }
      : { state: 'disabled', detail: '代理未启用，未执行健康探测' })
  ])))
}

function healthFromProbe(
  result: ServiceResult<UpstreamProxyProbeResponse>
): UpstreamProxyHealth {
  if (!result.ok) {
    return Object.freeze({ state: 'unhealthy', detail: result.error.message || '健康探测失败' })
  }

  const probe = result.data.result
  if (!probe?.udp_associate_ok) {
    return Object.freeze({ state: 'unhealthy', detail: probeFailureDetail(probe) })
  }

  return Object.freeze({
    state: 'healthy',
    detail: probe.diagnosis || '代理支持 SOCKS5 UDP Associate',
    durationMs: validDuration(probe.duration_ms)
  })
}

function probeFailureDetail(result: UpstreamProxyProbeResult | undefined): string {
  if (!result) return '探测响应未包含 UDP Associate 结果'
  return [result.diagnosis, result.hint, result.error]
    .map(value => value?.trim())
    .filter(Boolean)
    .join('；') || 'UDP Associate 探测失败'
}

function validDuration(value: number | undefined): number | undefined {
  return Number.isFinite(value) && Number(value) >= 0 ? Math.round(Number(value)) : undefined
}
