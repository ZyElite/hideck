import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

async function source(path: string) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('production Vue surfaces present the HiDeck brand consistently', async () => {
  const [app, shell, login, loading, header, commands, automation, settings, atConsole] = await Promise.all([
    source('../src/App.vue'),
    source('../src/layouts/AuthenticatedShell.vue'),
    source('../src/views/Login.vue'),
    source('../src/components/LoadingScreen.vue'),
    source('../src/components/PageHeader.vue'),
    source('../src/components/commands/CommandChat.vue'),
    source('../src/views/AutomaticTasks.vue'),
    source('../src/views/Settings.vue'),
    source('../src/components/DeviceAtTab.vue')
  ])
  const visibleSources = [app, shell, login, loading, header, commands, automation, settings, atConsole]

  for (const content of visibleSources) {
    assert.doesNotMatch(content, /VoHive|VOHIVE/)
  }
  assert.match(shell, /sidebar-brand-icon">H</)
  assert.match(shell, /sidebar-brand-title">HiDeck</)
  assert.match(shell, /topbar-product">HIDECK</)
  assert.match(login, /identity-mark">H</)
  assert.match(login, /登录 HiDeck/)
  assert.match(app, /HiDeck 最终用户许可与免责声明/)
  assert.match(commands, /HiDeck 命令会话/)
  assert.match(settings, /title="HiDeck Gateway"/)
})

test('frontend integration identifiers use the HiDeck namespace', async () => {
  const [app, systemService, websheet, sensitive, phoneSession, settingsStore] = await Promise.all([
    source('../src/App.vue'),
    source('../src/services/system.ts'),
    source('../src/components/CarrierWebsheetDialog.vue'),
    source('../src/composables/useSensitiveVisibility.ts'),
    source('../src/services/phone-session.ts'),
    source('../src/stores/settings.ts')
  ])

  assert.doesNotMatch(app, /disclaimer_agreed_at|shouldShowDisclaimer/)
  assert.match(app, /getDisclaimerStatus/)
  assert.match(systemService, /put<DisclaimerStatus>\('\/settings\/disclaimer'/)
  assert.match(websheet, /hideck-websheet-callback/)
  assert.match(websheet, /hideck-websheet-complete/)
  assert.match(websheet, /hideck-websheet/)
  assert.match(sensitive, /hideck_show_sensitive/)
  assert.match(phoneSession, /hideck_phone_control/)
  assert.match(settingsStore, /x-hideck-signature/)
  assert.doesNotMatch([app, systemService, websheet, sensitive, phoneSession, settingsStore].join('\n'), /vohive/i)
})
