import test from 'node:test'
import assert from 'node:assert/strict'
import {
  isManagedDeviceBackendSwitch,
  normalizeManagedDeviceBackend
} from '../src/utils/deviceBackend.ts'

test('normalizes only managed QMI and MBIM backends', () => {
  assert.equal(normalizeManagedDeviceBackend(' QMI '), 'qmi')
  assert.equal(normalizeManagedDeviceBackend('mbim'), 'mbim')
  assert.equal(normalizeManagedDeviceBackend('at'), null)
  assert.equal(normalizeManagedDeviceBackend('auto'), null)
})

test('requires an explicit QMI/MBIM transition', () => {
  assert.equal(isManagedDeviceBackendSwitch('qmi', 'mbim'), true)
  assert.equal(isManagedDeviceBackendSwitch('mbim', 'qmi'), true)
  assert.equal(isManagedDeviceBackendSwitch('qmi', 'qmi'), false)
  assert.equal(isManagedDeviceBackendSwitch('at', 'qmi'), false)
  assert.equal(isManagedDeviceBackendSwitch('auto', 'mbim'), false)
})
