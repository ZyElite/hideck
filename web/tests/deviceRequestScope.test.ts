import assert from 'node:assert/strict'
import test from 'node:test'
import { createDeviceRequestScope } from '../src/utils/deviceRequestScope.ts'

test('invalidates a device request when selection changes', () => {
  const scope = createDeviceRequestScope('wwan0')
  const request = scope.begin('wwan0')

  scope.invalidate('wwan1')

  assert.equal(scope.isCurrent(request, 'wwan1'), false)
  assert.equal(scope.isCurrent(request, 'wwan0'), false)
})

test('keeps only the latest request for the selected device current', () => {
  const scope = createDeviceRequestScope('wwan0')
  const first = scope.begin('wwan0')
  const second = scope.begin('wwan0')

  assert.equal(scope.isCurrent(first, 'wwan0'), false)
  assert.equal(scope.isCurrent(second, 'wwan0'), true)
})
