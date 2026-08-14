import assert from 'node:assert/strict'
import test from 'node:test'
import { maskSavedWeComURLs } from '../src/stores/settings'

test('masks every persisted WeCom URL and removes empty rows', () => {
  assert.deepEqual(
    maskSavedWeComURLs([
      'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=first',
      ' ',
      '********'
    ]),
    ['********', '********']
  )
})
