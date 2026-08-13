import assert from 'node:assert/strict'
import test from 'node:test'
import {
  countAddedTimelineRecords,
  isNearTimelineEnd,
  TIMELINE_FOLLOW_THRESHOLD_PX
} from '../src/utils/timelineFollow'

test('timeline follows only while the viewport remains near its latest record', () => {
  assert.equal(isNearTimelineEnd({ scrollTop: 536, clientHeight: 400, scrollHeight: 1000 }), true)
  assert.equal(isNearTimelineEnd({ scrollTop: 535, clientHeight: 400, scrollHeight: 1000 }), false)
  assert.equal(TIMELINE_FOLLOW_THRESHOLD_PX, 64)
})

test('timeline counts only records that were not already rendered', () => {
  assert.equal(countAddedTimelineRecords(['command-1'], ['command-1', 'command-2', 'balance-a']), 2)
  assert.equal(countAddedTimelineRecords(['command-1', 'command-2'], ['command-2', 'command-1']), 0)
  assert.equal(countAddedTimelineRecords([], []), 0)
})
