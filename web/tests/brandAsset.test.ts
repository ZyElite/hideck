import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

async function asset(path: string) {
  return readFile(new URL(path, import.meta.url), 'utf8')
}

test('browser title and favicon use the HiDeck identity', async () => {
  const [html, favicon] = await Promise.all([
    asset('../index.html'),
    asset('../public/favicon.svg')
  ])

  assert.match(html, /<title>HiDeck<\/title>/)
  assert.doesNotMatch(html, /VoHive/)
  assert.match(favicon, /id="brand-gradient"/)
  assert.match(favicon, /#1fa574/)
  assert.match(favicon, /M9\.5 9 H13 V14\.25 H19 V9 H22\.5 V23/)
  assert.doesNotMatch(favicon, /6366f1|a855f7|字母 V/)
})
