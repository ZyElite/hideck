import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

function source(relativePath: string): string {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8')
}

const dashboard = source('../src/views/Dashboard.vue')
const proxy = source('../src/views/Proxy.vue')
const automation = source('../src/views/AutomaticTasks.vue')
const sms = source('../src/views/Sms.vue')
const commands = source('../src/views/Commands.vue')
const proxyMode = source('../src/components/proxy/ProxyModeSwitch.vue')
const proxyInventory = source('../src/components/proxy/ProxyInventoryShell.vue')
const ruleEditor = source('../src/components/commands/RuleEditorDrawer.vue')
const taskList = source('../src/components/automation/AutomaticTaskList.vue')
const taskDetail = source('../src/components/automation/AutomaticTaskDetail.vue')

test('migrated business workspaces use one continuous device-style surface', () => {
  assert.match(proxy, /class="proxy-workspace ui-card"/)
  assert.match(sms, /class="flex-1 sms-workspace ui-card overflow-hidden relative"/)
  assert.match(commands, /class="commands-layout ui-card"/)
  assert.match(automation, /class="automation-shell ui-card"/)
})

test('proxy and automatic task content do not repeat the outer card layer', () => {
  assert.doesNotMatch(proxyMode, /class="proxy-mode-switch ui-card"/)
  assert.doesNotMatch(proxyInventory, /class="proxy-inventory ui-card"/)
  assert.doesNotMatch(taskList, /class="task-list-region ui-card"/)
  assert.doesNotMatch(taskDetail, /class="task-detail ui-card"/)
})

test('route-specific shells do not hardcode their own app canvas backgrounds', () => {
  assert.doesNotMatch(sms, /\.sms-page\s*\{[^}]*background:/s)
  assert.doesNotMatch(commands, /\.commands-page\s*\{[^}]*background:/s)
  assert.match(ruleEditor, /\.command-rule-drawer\) \{[^}]*background: var\(--ui-surface\);/)
})

test('dashboard remains outside this background-alignment change', () => {
  assert.match(dashboard, /class="app-page dashboard-page"/)
})
