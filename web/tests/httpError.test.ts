import assert from 'node:assert/strict'
import test from 'node:test'
import { toAppError } from '../src/services/http.ts'

test('preserves service error metadata when a Result error is rethrown', () => {
  const serviceError = {
    message: '设备详情读取失败',
    status: 503,
    method: 'get',
    url: '/devices/wwan0/overview',
    code: 'ERR_BACKEND_UNAVAILABLE'
  }

  assert.deepEqual(toAppError(serviceError), serviceError)
})
