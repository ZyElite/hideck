import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const commandsView = await readFile(new URL('../src/views/Commands.vue', import.meta.url), 'utf8')
const commandComposer = await readFile(new URL('../src/components/commands/CommandComposer.vue', import.meta.url), 'utf8')

test('command workspace source places balance controls before the conversation', () => {
  const balanceIndex = commandsView.indexOf('<BalancePanel')
  const conversationIndex = commandsView.indexOf('<main class="conversation-pane">')

  assert.ok(balanceIndex >= 0, 'balance panel is present')
  assert.ok(conversationIndex > balanceIndex, 'balance panel precedes the conversation')
  assert.match(commandsView, /grid-template-columns:\s*340px minmax\(0, 1fr\)/)
  assert.match(commandsView, /grid-template-rows:\s*minmax\(190px, 34%\) minmax\(0, 1fr\)/)
})

test('command composer source stays visible above the mobile safe area', () => {
  assert.match(commandComposer, /env\(safe-area-inset-bottom\)/)
  assert.match(commandComposer, /@media \(max-width: 1023px\).*position:\s*sticky;\s*bottom:\s*0;/s)
})
