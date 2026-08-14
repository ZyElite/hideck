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
const upstreamInventory = await readFile(
  new URL('../src/components/proxy/ProxyUpstreamInventory.vue', import.meta.url),
  'utf8'
)
const inventoryShell = await readFile(
  new URL('../src/components/proxy/ProxyInventoryShell.vue', import.meta.url),
  'utf8'
)
const emptyState = await readFile(
  new URL('../src/components/EmptyState.vue', import.meta.url),
  'utf8'
)

test('proxy page uses the compact production inventory workspace', () => {
  assert.match(proxyView, /<PageHeader title="代理管理"/)
  assert.match(proxyView, /<section class="proxy-workspace ui-card ui-workspace-glow">/)
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

test('both proxy modes share one compact inventory shell', () => {
  assert.match(upstreamInventory, /<ProxyInventoryShell/)
  assert.match(outboundInventory, /<ProxyInventoryShell/)
  assert.match(inventoryShell, /min-height: 76px/)
  assert.match(inventoryShell, /<EmptyState[\s\S]*compact/)
  assert.match(emptyState, /min-height: 156px/)
  assert.doesNotMatch(modeSwitch, /class="proxy-mode-switch ui-card"/)
  assert.doesNotMatch(inventoryShell, /class="proxy-inventory ui-card"/)
  assert.doesNotMatch(inventoryShell, /letter-spacing/)
  assert.doesNotMatch(upstreamInventory, /proxy-inventory-header/)
  assert.doesNotMatch(outboundInventory, /proxy-inventory-header/)
})

test('outbound editing never falls back to incomplete overview data', () => {
  const fetchIndex = proxyView.indexOf('await proxyStore.fetchInstance(inst.id)')
  const rejectIndex = proxyView.indexOf('if (!result.ok) throw result.error', fetchIndex)
  const openIndex = proxyView.indexOf('drawerOpen.value = true', rejectIndex)

  assert.ok(fetchIndex >= 0)
  assert.ok(rejectIndex > fetchIndex)
  assert.ok(openIndex > rejectIndex)
  assert.doesNotMatch(proxyView, /已使用概览数据/)
  assert.match(proxyView, /ElMessage\.error\(err\.message \|\| '读取完整实例配置失败'\)/)
})

test('only the latest upstream refresh owns page errors and loading flags', () => {
  const fetchStart = proxyView.indexOf('async function fetchUpstream')
  const requestStart = proxyView.indexOf('const request = upstreamRequestGate.begin()', fetchStart)
  const awaitStore = proxyView.indexOf('await upstreamStore.fetchAll()', requestStart)
  const staleResultGuard = proxyView.indexOf('if (!request.isCurrent()) return', awaitStore)
  const resultRead = proxyView.indexOf('const error = result.ok', staleResultGuard)
  const catchStart = proxyView.indexOf('} catch (e: unknown) {', resultRead)
  const staleErrorGuard = proxyView.indexOf('if (!request.isCurrent()) return', catchStart)
  const errorWrite = proxyView.indexOf('upstreamError.value = {', staleErrorGuard)
  const finallyStart = proxyView.indexOf('} finally {', errorWrite)
  const currentOwnerGuard = proxyView.indexOf('if (request.isCurrent()) {', finallyStart)
  const loadingClear = proxyView.indexOf('upstreamLoading.value = false', currentOwnerGuard)
  const refreshingClear = proxyView.indexOf('upstreamRefreshing.value = false', loadingClear)

  assert.ok(requestStart > fetchStart)
  assert.ok(awaitStore > requestStart)
  assert.ok(staleResultGuard > awaitStore)
  assert.ok(resultRead > staleResultGuard)
  assert.ok(staleErrorGuard > catchStart)
  assert.ok(errorWrite > staleErrorGuard)
  assert.ok(currentOwnerGuard > finallyStart)
  assert.ok(loadingClear > currentOwnerGuard)
  assert.ok(refreshingClear > loadingClear)
  assert.match(proxyView, /upstreamLoading\.value = isInitial/)
  assert.match(proxyView, /upstreamRefreshing\.value = !isInitial && !silent/)
})
