import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

function source(relativePath: string): string {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8')
}

const dashboard = source('../src/views/Dashboard.vue')
const globalStyles = source('../src/style.css')
const proxy = source('../src/views/Proxy.vue')
const automation = source('../src/views/AutomaticTasks.vue')
const sms = source('../src/views/Sms.vue')
const commands = source('../src/views/Commands.vue')
const phone = source('../src/views/Phone.vue')
const phoneHistory = source('../src/components/PhoneCallHistory.vue')
const logs = source('../src/views/Logs.vue')
const settings = source('../src/views/Settings.vue')
const proxyMode = source('../src/components/proxy/ProxyModeSwitch.vue')
const proxyInventory = source('../src/components/proxy/ProxyInventoryShell.vue')
const ruleEditor = source('../src/components/commands/RuleEditorDrawer.vue')
const taskList = source('../src/components/automation/AutomaticTaskList.vue')
const taskDetail = source('../src/components/automation/AutomaticTaskDetail.vue')

test('migrated business workspaces use one continuous device-style surface', () => {
  assert.match(proxy, /class="proxy-workspace ui-card ui-workspace-glow"/)
  assert.match(sms, /class="flex-1 sms-workspace ui-card ui-workspace-glow overflow-hidden relative"/)
  assert.match(commands, /class="commands-layout ui-card ui-workspace-glow"/)
  assert.match(automation, /class="automation-shell ui-card ui-workspace-glow"/)
  assert.match(phone, /class="phone-workspace ui-card ui-workspace-glow"/)
  assert.match(logs, /class="logs-workspace ui-card ui-workspace-glow"/)
  assert.match(settings, /class="settings-workspace-shell ui-card ui-workspace-glow"/)
})

test('continuous workspace content does not repeat the outer card layer', () => {
  assert.doesNotMatch(proxyMode, /class="proxy-mode-switch ui-card"/)
  assert.doesNotMatch(proxyInventory, /class="proxy-inventory ui-card"/)
  assert.doesNotMatch(taskList, /class="task-list-region ui-card"/)
  assert.doesNotMatch(taskDetail, /class="task-detail ui-card"/)
  assert.doesNotMatch(phone, /class="phone-console ui-panel"/)
  assert.doesNotMatch(phoneHistory, /class="history-panel ui-panel"/)
  assert.doesNotMatch(logs, /class="logs-control-rail ui-card"/)
  assert.doesNotMatch(logs, /class="log-frame ui-card/)
  assert.doesNotMatch(settings, /class="settings-security-card ui-card/)
  assert.doesNotMatch(settings, /class="settings-system-card ui-card/)
  assert.doesNotMatch(settings, /class="notify-card ui-card/)
})

test('continuous workspaces share the semantic primary glow', () => {
  assert.match(globalStyles, /\.ui-workspace-glow::before\s*\{[^}]*var\(--ui-primary\)/s)
  assert.match(globalStyles, /html\.dark \.ui-workspace-glow::before/)
})

test('SMS and command content let the workspace glow remain visible', () => {
  assert.match(sms, /\.sms-workspace :deep\(\.sms-device-rail\),[\s\S]*background: transparent;/)
  assert.match(commands, /\.commands-layout :deep\(\.chat-shell\) \{[^}]*background: transparent;/)
  assert.match(commands, /\.commands-layout :deep\(\.balance-rail\) \{[^}]*transparent/)
})

test('route-specific shells do not hardcode their own app canvas backgrounds', () => {
  assert.doesNotMatch(sms, /\.sms-page\s*\{[^}]*background:/s)
  assert.doesNotMatch(commands, /\.commands-page\s*\{[^}]*background:/s)
  assert.match(ruleEditor, /\.command-rule-drawer\) \{[^}]*background: var\(--ui-surface\);/)
})

test('dashboard remains outside this background-alignment change', () => {
  assert.match(dashboard, /class="app-page dashboard-page"/)
})
