import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const MIN_READABLE_FONT_SIZE = 12
const globalStyles = await readFile(new URL('../src/style.css', import.meta.url), 'utf8')
const dashboardTypographyFiles = [
  '../src/views/Dashboard.vue',
  '../src/components/DeviceCard.vue',
  '../src/components/ConnectionFocusStage.vue',
  '../src/components/TrafficAnalysisPanel.vue',
  '../src/components/PageHeader.vue',
  '../src/layouts/AuthenticatedShell.vue'
] as const
const deviceTypographyFiles = [
  '../src/views/Devices.vue',
  '../src/components/DeviceListPanel.vue',
  '../src/components/DeviceDetailHeader.vue',
  '../src/components/DeviceOverviewTab.vue',
  '../src/components/DeviceOverviewIdentityPanel.vue',
  '../src/components/DeviceOverviewConnectionStage.vue',
  '../src/components/DeviceEsimTab.vue',
  '../src/components/DeviceAtTab.vue',
  '../src/components/DeviceUssdTab.vue',
  '../src/components/DeviceConfigTab.vue',
  '../src/components/CardPolicyPanel.vue'
] as const
const proxyTypographyFiles = [
  '../src/views/Proxy.vue',
  '../src/components/proxy/ProxyInventoryShell.vue',
  '../src/components/proxy/ProxyModeSwitch.vue',
  '../src/components/proxy/ProxyOutboundInventory.vue',
  '../src/components/proxy/ProxyUpstreamInventory.vue',
  '../src/components/proxy/ProxyStatusBadge.vue',
  '../src/components/proxy/ProxyCountryRuleDrawer.vue',
  '../src/components/proxy/ProxyInstanceEditorDrawer.vue',
  '../src/components/proxy/ProxyUpstreamEditorDrawer.vue'
] as const
const smsTypographyFiles = [
  '../src/views/Sms.vue',
  '../src/components/sms/SmsDeviceRail.vue',
  '../src/components/sms/SmsThreadListPane.vue',
  '../src/components/sms/SmsConversationHeader.vue',
  '../src/components/sms/SmsMessageTimeline.vue',
  '../src/components/sms/SmsComposer.vue'
] as const
const commandTypographyFiles = [
  '../src/views/Commands.vue',
  '../src/components/commands/CommandChat.vue',
  '../src/components/commands/CommandTimeline.vue',
  '../src/components/commands/CommandComposer.vue',
  '../src/components/commands/CommandAudioPlayer.vue',
  '../src/components/commands/BalanceDrawer.vue',
  '../src/components/commands/BalancePanel.vue',
  '../src/components/commands/BalanceMessage.vue',
  '../src/components/commands/RuleEditorDrawer.vue'
] as const

const FONT_DECLARATION_PATTERNS = [
  /font-size:\s*([0-9.]+)px/g,
  /font:\s*[^;{}]*?([0-9.]+)px(?:\/[^\s;{}]+)?/g
] as const

function findUnreadableFontSizes(source: string): number[] {
  return FONT_DECLARATION_PATTERNS.flatMap((pattern) => {
    return Array.from(source.matchAll(pattern), (match) => Number(match[1]))
      .filter((size) => size < MIN_READABLE_FONT_SIZE)
  })
}

test('shared typography tokens follow the production design scale', () => {
  const expectedTokens = {
    caption: 12,
    'body-sm': 13,
    body: 14,
    title: 16,
    section: 20,
    'page-title': 24
  }

  for (const [name, size] of Object.entries(expectedTokens)) {
    assert.match(globalStyles, new RegExp(`--ui-font-${name}:\\s*${size}px`))
  }
})

test('typography audit detects declarations below the readable minimum', () => {
  assert.deepEqual(findUnreadableFontSizes('.metadata { font-size: 11px; }'), [11])
  assert.deepEqual(findUnreadableFontSizes('.code { font: 10px/1.4 monospace; }'), [10])
  assert.deepEqual(findUnreadableFontSizes('.body { font-size: 12px; }'), [])
})

test('dashboard text does not declare font sizes below 12px', async () => {
  for (const path of dashboardTypographyFiles) {
    const source = await readFile(new URL(path, import.meta.url), 'utf8')
    assert.deepEqual(findUnreadableFontSizes(source), [], path)
  }
})

test('device workspace text does not declare font sizes below 12px', async () => {
  for (const path of deviceTypographyFiles) {
    const source = await readFile(new URL(path, import.meta.url), 'utf8')
    assert.deepEqual(findUnreadableFontSizes(source), [], path)
  }
})

test('proxy workspace text does not declare font sizes below 12px', async () => {
  for (const path of proxyTypographyFiles) {
    const source = await readFile(new URL(path, import.meta.url), 'utf8')
    assert.deepEqual(findUnreadableFontSizes(source), [], path)
  }
})

test('SMS workspace text does not declare font sizes below 12px', async () => {
  for (const path of smsTypographyFiles) {
    const source = await readFile(new URL(path, import.meta.url), 'utf8')
    assert.deepEqual(findUnreadableFontSizes(source), [], path)
  }
})

test('command workspace text does not declare font sizes below 12px', async () => {
  for (const path of commandTypographyFiles) {
    const source = await readFile(new URL(path, import.meta.url), 'utf8')
    assert.deepEqual(findUnreadableFontSizes(source), [], path)
  }
})
