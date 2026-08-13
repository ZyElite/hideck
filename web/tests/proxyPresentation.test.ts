import assert from 'node:assert/strict'
import test from 'node:test'
import type { ProxyDevice, ProxyInstance, UpstreamProxy } from '../src/types/api.ts'
import {
  createOutboundProxyPresentation,
  createUpstreamProxyPresentation
} from '../src/utils/proxyPresentation.ts'

test('presents upstream proxy facts with real probe health and latency', () => {
  const proxy: UpstreamProxy = {
    id: 'uk-route',
    name: 'UK route',
    addr: '198.51.100.20:1080',
    username: 'route-user',
    password: '****',
    enabled: true
  }
  const before = { ...proxy }

  const presentation = createUpstreamProxyPresentation({
    proxy,
    ruleCount: 2,
    health: {
      state: 'healthy',
      detail: '代理支持标准 SOCKS5 UDP Associate',
      durationMs: 18
    }
  })

  assert.equal(presentation.name, 'UK route')
  assert.equal(presentation.address, '198.51.100.20:1080')
  assert.equal(presentation.enabledLabel, '已启用')
  assert.equal(presentation.healthLabel, 'UDP 可用 · 18 ms')
  assert.equal(presentation.healthTone, 'success')
  assert.equal(presentation.healthDetail, '代理支持标准 SOCKS5 UDP Associate')
  assert.equal(presentation.authenticationLabel, '账号认证')
  assert.equal(presentation.ruleCount, 2)
  assert.equal(Object.isFrozen(presentation), true)
  assert.deepEqual(proxy, before)
})

test('uses explicit upstream missing values and clamps invalid rule counts', () => {
  const presentation = createUpstreamProxyPresentation({
    proxy: { id: '', name: '', addr: '', username: '', enabled: false },
    ruleCount: -3
  })

  assert.equal(presentation.name, '未命名代理')
  assert.equal(presentation.address, '不可用')
  assert.equal(presentation.enabledLabel, '已禁用')
  assert.equal(presentation.healthLabel, '未启用')
  assert.equal(presentation.authenticationLabel, '免认证')
  assert.equal(presentation.ruleCount, 0)
})

test('keeps checking and failed upstream probes explicit', () => {
  const proxy: UpstreamProxy = {
    id: 'slow-route',
    name: 'Slow route',
    addr: '203.0.113.8:1080',
    username: '',
    enabled: true
  }

  const checking = createUpstreamProxyPresentation({
    proxy,
    ruleCount: 0,
    health: { state: 'checking', detail: '正在检测 SOCKS5 UDP Associate' }
  })
  const failed = createUpstreamProxyPresentation({
    proxy,
    ruleCount: 0,
    health: { state: 'unhealthy', detail: '代理明确拒绝了 UDP Associate' }
  })

  assert.equal(checking.healthLabel, '检测中')
  assert.equal(checking.healthTone, 'warning')
  assert.equal(failed.healthLabel, '探测失败')
  assert.equal(failed.healthTone, 'danger')
  assert.equal(failed.healthDetail, '代理明确拒绝了 UDP Associate')
})

test('presents outbound runtime, endpoint, authentication, and real device binding', () => {
  const instance: ProxyInstance = {
    id: 'proxy-wwan0',
    name: 'Primary egress',
    device_id: 'modem-1',
    enabled: true,
    mode: 'socks5',
    listen_addr: '0.0.0.0',
    listen_port: 1080,
    auth_enabled: false,
    username: ''
  }
  const device: ProxyDevice = { id: 'modem-1', name: 'EC25-01', interface: 'wwan0' }

  const presentation = createOutboundProxyPresentation({
    instance,
    device,
    status: { id: instance.id, running: true }
  })

  assert.equal(presentation.endpoint, '0.0.0.0:1080')
  assert.equal(presentation.runningLabel, '运行中')
  assert.equal(presentation.modeLabel, 'SOCKS5')
  assert.equal(presentation.authenticationLabel, '免认证')
  assert.equal(presentation.deviceLabel, 'EC25-01 / wwan0')
})

test('keeps a real outbound error visible instead of presenting success', () => {
  const presentation = createOutboundProxyPresentation({
    instance: {
      id: 'proxy-broken',
      name: '',
      device_id: '',
      enabled: true,
      mode: 'http',
      listen_addr: '',
      listen_port: 0,
      auth_enabled: true,
      username: 'admin'
    },
    status: { id: 'proxy-broken', running: true, last_error: 'bind failed' }
  })

  assert.equal(presentation.endpoint, '不可用')
  assert.equal(presentation.runningLabel, '运行异常')
  assert.equal(presentation.runningTone, 'danger')
  assert.equal(presentation.modeLabel, 'HTTP')
  assert.equal(presentation.authenticationLabel, '账号认证')
  assert.equal(presentation.deviceLabel, '未绑定')
  assert.equal(presentation.lastError, 'bind failed')
})
