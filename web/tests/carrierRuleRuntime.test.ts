import assert from 'node:assert/strict'
import test from 'node:test'
import { isCarrierRuleOperationBlocked } from '../src/utils/carrierRuleRuntime'

test('carrier rule operations are available only while every async state is idle', () => {
  assert.equal(isCarrierRuleOperationBlocked({ loading: false, saving: false, deletingId: '' }), false)
  assert.equal(isCarrierRuleOperationBlocked({ loading: true, saving: false, deletingId: '' }), true)
  assert.equal(isCarrierRuleOperationBlocked({ loading: false, saving: true, deletingId: '' }), true)
  assert.equal(isCarrierRuleOperationBlocked({ loading: false, saving: false, deletingId: 'custom-rule' }), true)
})
