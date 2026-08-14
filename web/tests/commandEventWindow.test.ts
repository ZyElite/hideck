import assert from 'node:assert/strict'
import test from 'node:test'
import type { CommandEvent } from '../src/types/commands'
import {
  mergeCommandEvents,
  PASSIVE_COMMAND_EVENT_LIMIT,
  retainLatestCommandEvents
} from '../src/utils/commandEventWindow'

function event(id: number, text = String(id)): CommandEvent {
  return {
    id,
    execution_id: `execution-${id}`,
    kind: 'result',
    text,
    created_at: new Date(id * 1000).toISOString()
  }
}

test('command events merge by id in chronological order without mutating inputs', () => {
  const current = [event(2), event(4, 'old')]
  const incoming = [event(1), event(4, 'updated'), event(3)]

  const merged = mergeCommandEvents(current, incoming)

  assert.deepEqual(merged.map((item) => item.id), [1, 2, 3, 4])
  assert.equal(merged.at(-1)?.text, 'updated')
  assert.equal(current.at(-1)?.text, 'old')
})

test('passive command event retention keeps only the latest bounded window', () => {
  const source = Array.from({ length: PASSIVE_COMMAND_EVENT_LIMIT + 2 }, (_, index) => event(index + 1))
  const retained = retainLatestCommandEvents(source, PASSIVE_COMMAND_EVENT_LIMIT)

  assert.equal(retained.dropped, true)
  assert.equal(retained.events.length, PASSIVE_COMMAND_EVENT_LIMIT)
  assert.equal(retained.events[0]?.id, 3)
  assert.equal(source.length, PASSIVE_COMMAND_EVENT_LIMIT + 2)
})
