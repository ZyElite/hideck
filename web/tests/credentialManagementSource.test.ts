import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const [login, settings, authStore, systemService] = await Promise.all([
  readFile(new URL('../src/views/Login.vue', import.meta.url), 'utf8'),
  readFile(new URL('../src/views/Settings.vue', import.meta.url), 'utf8'),
  readFile(new URL('../src/stores/auth.ts', import.meta.url), 'utf8'),
  readFile(new URL('../src/services/system.ts', import.meta.url), 'utf8')
])

test('login surfaces config and environment password remediation', () => {
  assert.match(login, /status\.change_required/)
  assert.match(login, /status\.management === 'environment'/)
  assert.match(login, /PROXY_WEB_PASSWORD/)
  assert.match(login, /\/settings\?focus=password/)
  assert.match(login, /当前密码仍是初始明文凭证或强度不足。建议立即修改。/)
  assert.doesNotMatch(login, /bcrypt|config\.yaml/)
})

test('settings disables ineffective changes for environment managed passwords', () => {
  assert.match(settings, /passwordManagedByEnvironment/)
  assert.match(settings, /控制台不会覆盖它/)
  assert.match(settings, /:disabled="!passwordStatus \|\| passwordManagedByEnvironment"/)
  assert.match(settings, /authStore\.applyToken\(result\.data\.token\)/)
})

test('credential APIs require typed login state and return replacement sessions', () => {
  assert.match(authStore, /isCredentialStatus/)
  assert.match(authStore, /登录响应缺少会话或凭证管理状态/)
  assert.match(systemService, /get<PasswordCredentialStatus>\('\/settings\/password'\)/)
  assert.match(systemService, /post<PasswordChangeResponse>\('\/settings\/password'/)
})
