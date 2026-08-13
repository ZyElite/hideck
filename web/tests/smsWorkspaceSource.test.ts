import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const smsView = readFileSync(new URL('../src/views/Sms.vue', import.meta.url), 'utf8')
const deviceRail = readFileSync(new URL('../src/components/sms/SmsDeviceRail.vue', import.meta.url), 'utf8')
const threadList = readFileSync(new URL('../src/components/sms/SmsThreadListPane.vue', import.meta.url), 'utf8')
const conversationHeader = readFileSync(new URL('../src/components/sms/SmsConversationHeader.vue', import.meta.url), 'utf8')
const messageTimeline = readFileSync(new URL('../src/components/sms/SmsMessageTimeline.vue', import.meta.url), 'utf8')
const composer = readFileSync(new URL('../src/components/sms/SmsComposer.vue', import.meta.url), 'utf8')
const smsService = readFileSync(new URL('../src/services/sms.ts', import.meta.url), 'utf8')

test('SMS workspace uses one continuous shell with dedicated navigation panes', () => {
  assert.match(smsView, /<SmsDeviceRail/)
  assert.match(smsView, /<SmsThreadListPane/)
  assert.match(smsView, /grid-template-columns:\s*218px 310px minmax\(0, 1fr\)/)
  assert.doesNotMatch(smsView, /class="sms-action-row"/)
  assert.match(smsView, /class="[^"]*sms-workspace/)
})

test('device rail exposes textual online state and accessible selection', () => {
  assert.match(deviceRail, /:aria-label="item\.accessibilityLabel"/)
  assert.match(deviceRail, /<small>\{\{ item\.detail \}\}<\/small>/)
  assert.match(deviceRail, /:aria-pressed="selectedId === item\.id"/)
})

test('thread pane preserves virtual scrolling and all production entries', () => {
  assert.match(threadList, /<RecycleScroller/)
  assert.match(threadList, /emit\('newMessage'\)/)
  assert.match(threadList, /emit\('select', thread\.key\)/)
  assert.match(threadList, /emit\('delete', thread\)/)
  assert.match(threadList, /LONG_PRESS_MS = 450/)
})

test('conversation pane exposes real runtime context and explicit message status', () => {
  assert.match(smsView, /createSmsConversationContext/)
  assert.match(conversationHeader, /context\.operatorLabel/)
  assert.match(conversationHeader, /context\.smsLabel/)
  assert.match(conversationHeader, /context\.imsLabel/)
  assert.match(messageTimeline, /if \(message\.status === 2\) return '已发送'/)
  assert.match(messageTimeline, /if \(message\.status === 3\) return '发送失败'/)
  assert.match(messageTimeline, /max-width: 64%/)
})

test('conversation actions remain wired to production handlers', () => {
  assert.match(smsView, /@refresh="\(\) => void fetchThreadLatest\(false\)"/)
  assert.match(smsView, /@delete="selectedThread && void confirmDeleteThread\(selectedThread\)"/)
  assert.match(smsView, /@load-more="loadMoreHistory"/)
  assert.match(smsView, /@delete="\(message\) => void confirmDeleteMessage\(message\)"/)
  assert.match(smsView, /@send="sendToCurrentThread"/)
  assert.match(composer, /Shift\+Enter 换行/)
})

test('narrow SMS workspace keeps all three panes visible in the prototype composition', () => {
  assert.doesNotMatch(smsView, /v-if="showDeviceSidebar"/)
  assert.doesNotMatch(smsView, /v-if="showListPane"/)
  assert.doesNotMatch(smsView, /v-if="showDetailPane"/)
  assert.match(smsView, /grid-template-columns:\s*64px minmax\(0, 1fr\)/)
  assert.match(smsView, /grid-template-rows:\s*250px minmax\(620px, 1fr\)/)
  assert.match(smsView, /grid-template-columns:\s*52px minmax\(0, 1fr\)/)
  assert.match(smsView, /@media \(prefers-reduced-motion: reduce\)/)
})

test('unread presentation and history failures stay explicit', () => {
  assert.match(smsService, /unreadCount: normalizeSmsUnreadCount\(contact\.unread_count\)/)
  assert.match(smsView, /:items="presentedThreads"/)
  assert.match(smsView, /unreadCount: isThreadUnreadForDisplay\(thread\)/)
  assert.match(smsView, /messagesError\.value = result\.error/)
  assert.match(smsView, /messagesError\.value = toAppError\(error\)/)
  assert.doesNotMatch(smsView, /Ignore history load errors/)
  assert.match(messageTimeline, /当前接口未返回此会话的历史短信/)
})

test('switching conversations clears prior content before requesting the next thread', () => {
  const selectionStart = smsView.indexOf('async function selectThread')
  const selectionEnd = smsView.indexOf('async function applyThreadSeen', selectionStart)
  const selectionSource = smsView.slice(selectionStart, selectionEnd)
  const keyUpdate = selectionSource.indexOf('selectedThreadKey.value = key')
  const contentReset = selectionSource.indexOf('threadMessages.value = []')
  const nextRequest = selectionSource.indexOf('await fetchThreadLatest(silent)')

  assert.ok(keyUpdate >= 0)
  assert.ok(contentReset > keyUpdate)
  assert.ok(nextRequest > contentReset)
  assert.match(smsView, /const requestThreadKey = selectedThreadKey\.value/)
  assert.match(smsView, /if \(selectedThreadKey\.value !== requestThreadKey\) return/)
})
