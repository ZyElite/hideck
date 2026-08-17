import assert from 'node:assert/strict'
import test from 'node:test'
import {
  firstRemainingDeviceId,
  isPCSCServiceUnavailable,
  routeDeviceStillManaged,
  suggestedAddDeviceId
} from '../src/utils/deviceSelection.ts'

test('route selection ignores a deleted device id once the list is known', () => {
  assert.equal(routeDeviceStillManaged('wwan0', '', ['wwan1']), false)
  assert.equal(routeDeviceStillManaged('wwan1', '', ['wwan1']), true)
  assert.equal(routeDeviceStillManaged('wwan0', 'wwan0', ['wwan1']), false)
})

test('route selection can apply before the list has loaded', () => {
  assert.equal(routeDeviceStillManaged('wwan0', '', []), true)
})

test('first remaining device prefers the current id when it still exists', () => {
  assert.equal(firstRemainingDeviceId(['wwan1'], 'wwan0'), 'wwan1')
  assert.equal(firstRemainingDeviceId(['wwan0', 'wwan1'], 'wwan1'), 'wwan1')
  assert.equal(firstRemainingDeviceId([], 'wwan0'), '')
})

test('suggested add id prefers the network interface', () => {
  assert.equal(suggestedAddDeviceId({ net_interface: 'wwan0', reader_name: 'Reader 00' }), 'wwan0')
  assert.equal(suggestedAddDeviceId({ reader_name: 'Example Reader 00 00' }), 'Example_Reader_00_00')
  assert.equal(suggestedAddDeviceId(null), '')
})

test('pcsc service-unavailable is not treated as a fatal discovery error', () => {
  assert.equal(
    isPCSCServiceUnavailable('pcsc: service is unavailable: pcsc: SCardEstablishContext failed with code 0x8010001D'),
    true
  )
  assert.equal(isPCSCServiceUnavailable('pcsc: reader not found'), false)
})
