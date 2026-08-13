import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const smsView = readFileSync(new URL('../src/views/Sms.vue', import.meta.url), 'utf8')
const deviceRail = readFileSync(new URL('../src/components/sms/SmsDeviceRail.vue', import.meta.url), 'utf8')
const threadList = readFileSync(new URL('../src/components/sms/SmsThreadListPane.vue', import.meta.url), 'utf8')

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
