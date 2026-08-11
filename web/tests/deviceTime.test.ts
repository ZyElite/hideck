import assert from 'node:assert/strict'
import test from 'node:test'
import {
  configureDeviceTime,
  deviceNow,
  formatDeviceDateTime,
  formatDeviceTime,
  resetDeviceTime
} from '../src/utils/deviceTime.ts'

test.afterEach(() => resetDeviceTime())

test('formats absolute timestamps in the device IANA timezone', () => {
  configureDeviceTime({
    now: '2026-08-11T12:00:00+08:00',
    timezone: 'Asia/Shanghai',
    offset_seconds: 28800,
    source: 'etc_timezone'
  }, 0, 0)

  assert.equal(formatDeviceDateTime('2026-08-11T04:30:15Z'), '2026-08-11 12:30:15')
})

test('preserves historical device-local log timestamps without a zone', () => {
  configureDeviceTime({
    now: '2026-08-11T12:00:00+08:00',
    timezone: 'Asia/Shanghai',
    offset_seconds: 28800,
    source: 'runtime_location'
  }, 0, 0)

  assert.equal(formatDeviceDateTime('2026-08-11 04:11:14'), '2026-08-11 04:11:14')
})

test('uses the explicit fixed offset when no IANA zone is available', () => {
  configureDeviceTime({
    now: '2026-08-11T09:30:00+05:30',
    timezone: '',
    offset_seconds: 19800,
    source: 'fixed_offset'
  }, 0, 0)

  assert.equal(formatDeviceTime('2026-08-11T04:00:00Z'), '09:30:00')
})

test('calibrates browser-generated timestamps against the device clock', () => {
  configureDeviceTime({
    now: '1970-01-01T00:00:10Z',
    timezone: 'UTC',
    offset_seconds: 0,
    source: 'runtime_location'
  }, 1000, 3000)

  assert.equal(deviceNow(4000), 12000)
  assert.equal(formatDeviceTime(4000, { clientClock: true }), '00:00:12')
})

test('rejects an invalid named timezone instead of using the browser timezone', () => {
  assert.throws(() => configureDeviceTime({
    now: '2026-08-11T12:00:00+08:00',
    timezone: 'not/a-zone',
    offset_seconds: 28800,
    source: 'env_tz'
  }, 0, 0), /无效 IANA 时区/)
})
