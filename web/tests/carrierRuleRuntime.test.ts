import assert from 'node:assert/strict'
import test from 'node:test'
import type { CarrierQueryRule } from '../src/types/commands'
import { editableCarrierRule, effectiveCarrierRules, isCarrierRuleOperationBlocked } from '../src/utils/carrierRuleRuntime'

function rule(overrides: Partial<CarrierQueryRule> = {}): CarrierQueryRule {
  return {
    id: 'carrier', mcc: '234', mnc: '10', operator: 'Carrier', transport: 'sms',
    destination: '123', payload: 'BAL', response_mode: 'sms', expected_senders: ['123'],
    cost_status: 'unknown', evidence_type: 'user', enabled: true, ...overrides
  }
}

test('carrier rule operations are available only while every async state is idle', () => {
  assert.equal(isCarrierRuleOperationBlocked({ loading: false, saving: false, deletingId: '' }), false)
  assert.equal(isCarrierRuleOperationBlocked({ loading: true, saving: false, deletingId: '' }), true)
  assert.equal(isCarrierRuleOperationBlocked({ loading: false, saving: true, deletingId: '' }), true)
  assert.equal(isCarrierRuleOperationBlocked({ loading: false, saving: false, deletingId: 'custom-rule' }), true)
})

test('built-in editing creates a database override and prefers an existing override', () => {
  const builtIn = rule({ operator: 'Built in', built_in: true })
  const editable = editableCarrierRule(builtIn, [])
  assert.equal(editable.built_in, false)
  assert.equal(editable.id, builtIn.id)

  const custom = rule({ operator: 'Custom override', expected_senders: ['reply'] })
  const existing = editableCarrierRule(builtIn, [custom])
  assert.equal(existing.operator, 'Custom override')
  existing.expected_senders?.push('changed')
  assert.deepEqual(custom.expected_senders, ['reply'])
})

test('effective rules replace a built-in row with its database override', () => {
  const builtIn = rule({ operator: 'Built in', built_in: true })
  const custom = rule({ operator: 'Custom override' })
  const another = rule({ id: 'another', operator: 'Another', built_in: true })
  assert.deepEqual(
    effectiveCarrierRules([builtIn, another], [custom]).map((item) => item.operator),
    ['Custom override', 'Another']
  )
})

test('disabled or retargeted overrides keep the built-in rule suppressed by ID', () => {
  const builtIn = rule({ id: 'same', mcc: '234', mnc: '10', built_in: true })
  const disabled = rule({ id: 'same', mcc: '234', mnc: '10', enabled: false })
  assert.deepEqual(effectiveCarrierRules([builtIn], [disabled]), [])

  const retargeted = rule({ id: 'same', mcc: '530', mnc: '24', operator: 'Retargeted' })
  assert.deepEqual(effectiveCarrierRules([builtIn], [retargeted]).map((item) => item.operator), ['Retargeted'])
})
