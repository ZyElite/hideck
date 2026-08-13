import assert from 'node:assert/strict'
import test from 'node:test'
import { readFile } from 'node:fs/promises'

const phoneView = await readFile(new URL('../src/views/Phone.vue', import.meta.url), 'utf8')
const phoneCSS = await readFile(new URL('../src/styles/phone.css', import.meta.url), 'utf8')
const phoneStore = await readFile(new URL('../src/stores/phone.ts', import.meta.url), 'utf8')
const dialPad = await readFile(new URL('../src/components/PhoneDialPad.vue', import.meta.url), 'utf8')
const callBar = await readFile(new URL('../src/components/PhoneCallBar.vue', import.meta.url), 'utf8')
const shell = await readFile(new URL('../src/layouts/AuthenticatedShell.vue', import.meta.url), 'utf8')
const router = await readFile(new URL('../src/router/index.ts', import.meta.url), 'utf8')

test('phone route and navigation remain available outside the phone page', () => {
  assert.match(router, /path: '\/phone'/)
  assert.match(shell, /<PhoneCallBar\s*\/>/)
  assert.match(shell, /index: '\/phone', label: '电话'/)
  assert.match(shell, /v-for="item in mobileMenuItems"/)
})

test('dial pad is an explicit fixed 3 by 4 control with accessible buttons', () => {
  assert.match(dialPad, /grid-template-columns: repeat\(3, 1fr\)/)
  assert.match(dialPad, /width: 276px/)
  assert.match(dialPad, /height: 62px/)
  assert.match(dialPad, /:aria-label=/)
  for (const digit of ['0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '*', '#']) {
    assert.match(dialPad, new RegExp(`digit: '${digit.replace('*', '\\*')}'`))
  }
})

test('phone view exposes listen-only and microphone call actions without hiding the limitation', () => {
  assert.match(phoneView, /下载 CA 证书/)
  assert.match(phoneView, /麦克风需要受信任的 HTTPS/)
  assert.match(phoneView, /当前 HTTP 页面仍可“仅听接听”来电/)
  assert.match(phoneView, />拒接</)
  assert.match(phoneView, />仅听接听</)
  assert.match(phoneView, />麦克风接听</)
  assert.match(phoneView, /对方听不到你/)
  assert.match(phoneView, /@click="answerListenOnly\(call\)"/)
  assert.match(phoneView, /@click="enableListenOnlyMedia"/)
  assert.match(phoneStore, /answerListenOnly\(call: PhoneCall\)/)
  assert.match(phoneStore, /prepare\(\{ microphone: false \}\)/)
  assert.match(phoneStore, /mediaMode = 'listen-only'/)
  assert.match(phoneView, /取消静音/)
  assert.match(phoneView, />键盘</)
  assert.match(phoneView, />挂断</)
  assert.match(phoneView, /sendDTMF\(digit\)/)
  assert.match(phoneView, /<PhoneCallHistory/)
  assert.match(callBar, /aria-live="polite"/)
})

test('phone layout has responsive single-column and 44px touch targets', () => {
  assert.match(phoneCSS, /@media \(max-width: 900px\)[\s\S]*grid-template-columns: 1fr/)
  assert.match(phoneCSS, /min-height: 48px/)
  assert.match(callBar, /width: 44px; height: 44px/)
  assert.match(shell, /repeat\(5, minmax\(44px, 1fr\)\)/)
})
