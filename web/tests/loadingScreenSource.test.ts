import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('../src/components/LoadingScreen.vue', import.meta.url), 'utf8')

test('loading state uses a lightweight workspace surface instead of a notification card', () => {
  assert.match(source, /class="loading-screen" role="status" aria-live="polite"/)
  assert.match(source, /class="loading-state"/)
  assert.match(source, /VOHIVE CONTROL PLANE/)
  assert.doesNotMatch(source, /loading-panel|loading-mark|loading-spinner/)
})

test('loading state motion is brief, transform-opacity only, and reduced-motion safe', () => {
  assert.match(source, /animation: loading-state-enter 220ms var\(--ui-ease-out\) both/)
  assert.match(source, /@keyframes loading-state-enter\s*\{[^}]*opacity: 0; transform: translateY\(6px\)/s)
  assert.doesNotMatch(source, /animation:[^;]*infinite/)
  assert.match(source, /@media \(prefers-reduced-motion: reduce\)[\s\S]*animation-name: loading-state-fade/)
})
