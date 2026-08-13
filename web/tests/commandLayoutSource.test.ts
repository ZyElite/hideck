import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const commandsView = await readFile(new URL('../src/views/Commands.vue', import.meta.url), 'utf8')
const balanceDrawer = await readFile(new URL('../src/components/commands/BalanceDrawer.vue', import.meta.url), 'utf8')
const commandChat = await readFile(new URL('../src/components/commands/CommandChat.vue', import.meta.url), 'utf8')
const commandTimeline = await readFile(new URL('../src/components/commands/CommandTimeline.vue', import.meta.url), 'utf8')
const commandAudioPlayer = await readFile(new URL('../src/components/commands/CommandAudioPlayer.vue', import.meta.url), 'utf8')
const commandComposer = await readFile(new URL('../src/components/commands/CommandComposer.vue', import.meta.url), 'utf8')

test('command workspace keeps balance inside chat and a responsive history drawer', () => {
  const chatIndex = commandsView.indexOf('<CommandChat')

  assert.ok(chatIndex >= 0, 'independent chat is present')
  assert.match(commandsView, /<BalanceDrawer/)
  assert.match(balanceDrawer, /<el-drawer[\s\S]*<BalancePanel/)
  assert.match(balanceDrawer, /narrowViewport\.value \? 'btt' : 'rtl'/)
  assert.match(commandChat, /Wallet24Regular/)
  assert.match(commandChat, /:events="visibleEvents"/)
  assert.match(commandChat, /:balance-queries="visibleBalanceQueries"/)
  assert.match(commandTimeline, /<BalanceMessage v-else/)
})

test('VoWiFi command results render authenticated MP3 playback', () => {
  assert.match(commandTimeline, /<CommandAudioPlayer/)
  assert.match(commandAudioPlayer, /commandService\.recording/)
  assert.match(commandAudioPlayer, /URL\.createObjectURL/)
  assert.match(commandAudioPlayer, /URL\.revokeObjectURL/)
  assert.match(commandAudioPlayer, /<audio[^>]*controls[^>]*preload="metadata"/)
})

test('command conversation uses the Studio event rail instead of chat bubbles', () => {
  assert.match(commandChat, /class="chat-title-icon"/)
  assert.match(commandChat, /实时连接/)
  assert.match(commandChat, /class="device-target"/)
  assert.match(commandTimeline, /class="timeline-track"/)
  assert.match(commandTimeline, /class="event-marker"/)
  assert.match(commandTimeline, /`tone-\$\{item\.presentation\.tone\}`/)
  assert.doesNotMatch(commandTimeline, /class="message"/)
})

test('command composer source stays visible above the mobile safe area', () => {
  assert.match(commandComposer, /env\(safe-area-inset-bottom\)/)
  assert.match(commandComposer, /@media \(max-width: 1023px\).*position:\s*sticky;\s*bottom:\s*0;/s)
})

test('command realtime stream starts before secondary page data finishes', () => {
  const pageDataIndex = commandsView.indexOf('const pageData = Promise.all')
  const historyIndex = commandsView.indexOf('await loadEvents()')
  const connectIndex = commandsView.indexOf('void stream.connect()')
  const pageDataAwaitIndex = commandsView.indexOf('await pageData')

  assert.ok(pageDataIndex >= 0)
  assert.ok(historyIndex > pageDataIndex)
  assert.ok(connectIndex > historyIndex)
  assert.ok(pageDataAwaitIndex > connectIndex)
  assert.match(commandsView, /await loadEvents\(\)\s+if \(disposed\) return/)
  assert.match(commandsView, /await pageData\s+if \(disposed\) return/)
  assert.match(commandsView, /onUnmounted\(\(\) => \{\s+disposed = true/)
})
