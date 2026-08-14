import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const settings = await readFile(new URL('../src/views/Settings.vue', import.meta.url), 'utf8')

test('settings exposes real update checking and Docker redeployment instructions', () => {
  assert.match(settings, /systemService\.checkUpdate\(\)/)
  assert.match(settings, /updateInfo\.is_docker \? '查看 Docker 更新方法'/)
  assert.match(settings, /docker compose pull\\ndocker compose up -d/)
  assert.doesNotMatch(settings, /systemService\.applyUpdate\(\)/)
  assert.doesNotMatch(settings, /立即更新并重启/)
  assert.doesNotMatch(settings, /dangerouslyUseHTMLString: true[\s\S]*release_note/)
})
