import assert from 'node:assert/strict'
import test from 'node:test'
import { useEventStream } from '../src/composables/useEventStream'

test('event stream preserves an explicit zero resume cursor', () => {
  const stream = useEventStream({
    path: '/events',
    eventName: 'event',
    parse: (payload) => payload,
    onEvent: () => undefined
  })

  stream.setLastEventId(0)
  assert.equal(stream.lastEventId.value, '0')

  stream.setLastEventId('')
  assert.equal(stream.lastEventId.value, '')
})
