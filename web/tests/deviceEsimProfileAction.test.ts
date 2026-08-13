import assert from 'node:assert/strict'
import test from 'node:test'
import { esimProfileActionForState } from '../src/components/deviceEsimProfileAction'

test('enabled profile uses the disable operation', () => {
  assert.equal(esimProfileActionForState(1), 'disable')
})

test('non-enabled profile uses the switch operation', () => {
  assert.equal(esimProfileActionForState(0), 'switch')
  assert.equal(esimProfileActionForState(2), 'switch')
})
