import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const commandsView = await readFile(new URL('../src/views/Commands.vue', import.meta.url), 'utf8')
const balanceDrawer = await readFile(new URL('../src/components/commands/BalanceDrawer.vue', import.meta.url), 'utf8')
const commandChat = await readFile(new URL('../src/components/commands/CommandChat.vue', import.meta.url), 'utf8')
const commandTimeline = await readFile(new URL('../src/components/commands/CommandTimeline.vue', import.meta.url), 'utf8')
const commandAudioPlayer = await readFile(new URL('../src/components/commands/CommandAudioPlayer.vue', import.meta.url), 'utf8')
const commandComposer = await readFile(new URL('../src/components/commands/CommandComposer.vue', import.meta.url), 'utf8')

test('command workspace keeps an inline responsive balance rail beside the conversation', () => {
  const chatIndex = commandsView.indexOf('<CommandChat')
  const balanceIndex = commandsView.indexOf('<BalanceDrawer')

  assert.ok(chatIndex >= 0, 'independent chat is present')
  assert.ok(balanceIndex > chatIndex, 'balance rail follows chat inside the workspace')
  assert.match(balanceDrawer, /class="balance-rail"/)
  assert.match(balanceDrawer, /<BalancePanel/)
  assert.doesNotMatch(balanceDrawer, /<el-drawer/)
  assert.match(commandsView, /grid-template-columns:\s*minmax\(0, 1fr\) clamp\(300px, 25vw, 360px\)/)
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
  assert.match(commandComposer, /@media \(max-width: 820px\).*bottom:\s*calc\(72px \+ env\(safe-area-inset-bottom\)\)/s)
  assert.match(commandComposer, /v-for="definition in definitions"/)
  assert.match(commandComposer, /definition\.dangerous/)
  assert.match(commandComposer, /retargetDeviceCommand/)
  assert.match(commandComposer, /prefers-reduced-motion: reduce/)
})

test('command workspace stacks cleanly at the application mobile boundary', () => {
  assert.match(commandsView, /@media \(max-width: 1023px\)[\s\S]*grid-template-columns:\s*minmax\(0, 1fr\)/)
  assert.match(commandsView, /height:\s*690px;\s*min-height:\s*690px/)
  assert.match(balanceDrawer, /@media \(max-width: 1023px\)[\s\S]*border-top:[^;]+;\s*border-left:\s*0;/)
  assert.match(commandChat, /@media \(max-width: 640px\)[\s\S]*grid-template-areas:\s*"heading" "device" "actions"/)
  assert.match(commandTimeline, /@media \(prefers-reduced-motion: reduce\)/)
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

test('command timeline opens at the latest record without stealing historical reading position', () => {
  assert.match(commandTimeline, /ref="timelineScroll"/)
  assert.match(commandTimeline, /captureVisibleAnchor/)
  assert.match(commandTimeline, /restoreVisibleAnchor/)
  assert.match(commandTimeline, /followingLatest\.value/)
  assert.match(commandTimeline, /\{\{ pendingRecordCount \}\} 条新记录 · 查看最新/)
  assert.match(commandTimeline, /prefers-reduced-motion: reduce/)
  assert.match(commandChat, /:history-version="historyVersion"/)
  assert.match(commandsView, /historyVersion\.value \+= 1/)
})

test('command runtime state refreshes periodically and exposes a unified manual refresh', () => {
  assert.match(commandsView, /runtimeStatus\.startPolling\(\)/)
  assert.match(commandsView, /runtimeStatus\.refresh\(\{ background: true \}\)/)
  assert.match(commandsView, /@refresh="refreshAll"/)
  assert.match(commandsView, /stream\.lastError\.value/)
  assert.match(commandChat, /aria-label="刷新命令与状态"/)
  assert.match(commandChat, /:loading="refreshing"/)
  assert.match(commandChat, /class="sync-warning" role="status"/)
  assert.match(commandChat, /:context-key="`\$\{selectedDevice\}:\$\{refreshVersion\}`"/)
})
