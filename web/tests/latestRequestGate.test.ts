import assert from 'node:assert/strict'
import test from 'node:test'
import { createLatestRequestGate } from '../src/utils/latestRequestGate'

test('only the latest request ticket remains current', () => {
  const gate = createLatestRequestGate()
  const first = gate.begin()

  assert.equal(first.isCurrent(), true)

  const second = gate.begin()

  assert.equal(first.isCurrent(), false)
  assert.equal(second.isCurrent(), true)
})
