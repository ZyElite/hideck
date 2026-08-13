import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const proxyView = await readFile(new URL('../src/views/Proxy.vue', import.meta.url), 'utf8')
const modeSwitch = await readFile(
  new URL('../src/components/proxy/ProxyModeSwitch.vue', import.meta.url),
  'utf8'
)
const outboundInventory = await readFile(
  new URL('../src/components/proxy/ProxyOutboundInventory.vue', import.meta.url),
  'utf8'
)

test('proxy page uses the compact production inventory workspace', () => {
  assert.match(proxyView, /<PageHeader title="代理管理"/)
  assert.match(proxyView, /<ProxyModeSwitch/)
  assert.match(proxyView, /<ProxyUpstreamInventory/)
  assert.match(proxyView, /<ProxyOutboundInventory/)
  assert.match(proxyView, /<ProxyCountryRuleDrawer/)
  assert.match(proxyView, /title="加载前置代理失败"/)
  assert.match(proxyView, /title="加载代理配置失败"/)
  assert.doesNotMatch(proxyView, /WorkspaceStage|proxy-workspace-stage/)
  assert.doesNotMatch(proxyView, /uk\.proxy|爱尔兰代理|日本代理节点/)
})

test('proxy mode change is transform-opacity only and reduced-motion safe', () => {
  assert.match(proxyView, /transition: transform 180ms[^;]+, opacity 180ms/)
  assert.match(proxyView, /transition: transform 120ms[^;]+, opacity 120ms/)
  assert.match(proxyView, /@media \(prefers-reduced-motion: reduce\)/)
  assert.match(proxyView, /transition-property: opacity/)
  assert.match(proxyView, /transform: none/)
  assert.doesNotMatch(proxyView, /transition:\s*all/)
})

test('proxy controls retain real runtime facts and accessible actions', () => {
  assert.match(modeSwitch, /aria-current/)
  assert.match(modeSwitch, /min-height: 58px/)
  assert.match(outboundInventory, /row\.lastError/)
  assert.match(outboundInventory, /:disabled="!row\.enabled"/)

  for (const action of ['启动', '停止', '重启', '编辑', '删除']) {
    assert.ok(outboundInventory.includes(`aria-label="\`${action}`))
  }
})
