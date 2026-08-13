import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const MIN_READABLE_FONT_SIZE = 12
const globalStyles = await readFile(new URL('../src/style.css', import.meta.url), 'utf8')

const FONT_DECLARATION_PATTERNS = [
  /font-size:\s*([0-9.]+)px/g,
  /font:\s*[^;{}]*?([0-9.]+)px(?:\/[^\s;{}]+)?/g
] as const

function findUnreadableFontSizes(source: string): number[] {
  return FONT_DECLARATION_PATTERNS.flatMap((pattern) => {
    return Array.from(source.matchAll(pattern), (match) => Number(match[1]))
      .filter((size) => size < MIN_READABLE_FONT_SIZE)
  })
}

test('shared typography tokens follow the production design scale', () => {
  const expectedTokens = {
    caption: 12,
    'body-sm': 13,
    body: 14,
    title: 16,
    section: 20,
    'page-title': 24
  }

  for (const [name, size] of Object.entries(expectedTokens)) {
    assert.match(globalStyles, new RegExp(`--ui-font-${name}:\\s*${size}px`))
  }
})

test('typography audit detects declarations below the readable minimum', () => {
  assert.deepEqual(findUnreadableFontSizes('.metadata { font-size: 11px; }'), [11])
  assert.deepEqual(findUnreadableFontSizes('.code { font: 10px/1.4 monospace; }'), [10])
  assert.deepEqual(findUnreadableFontSizes('.body { font-size: 12px; }'), [])
})
