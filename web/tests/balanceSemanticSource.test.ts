import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const balancePanel = await readFile(new URL('../src/components/commands/BalancePanel.vue', import.meta.url), 'utf8')
const balanceMessage = await readFile(new URL('../src/components/commands/BalanceMessage.vue', import.meta.url), 'utf8')

test('balance parsed and success states keep distinct icon and color semantics', () => {
  assert.match(balancePanel, /tone === 'parsed'\"><Chat24Regular/)
  assert.match(balancePanel, /history-icon\.parsed, \.tone-parsed \{ color: var\(--ui-info\); \}/)
  assert.match(balanceMessage, /\.balance-message \{[^}]*color: var\(--ui-success\);/)
  assert.match(balanceMessage, /\.tone-success \{ color: var\(--ui-success\); \}/)
  assert.match(balanceMessage, /\.tone-parsed \{ color: var\(--ui-info\); \}/)
})
