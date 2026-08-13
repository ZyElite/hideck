import assert from 'node:assert/strict'
import test from 'node:test'
import { formatPlaybackTime, playbackProgress } from '../src/utils/audioPlayback'

test('formats finite playback positions for compact media controls', () => {
  assert.equal(formatPlaybackTime(0), '0:00')
  assert.equal(formatPlaybackTime(65.9), '1:05')
  assert.equal(formatPlaybackTime(3661), '1:01:01')
  assert.equal(formatPlaybackTime(Number.NaN), '0:00')
})

test('keeps playback progress inside the visible track', () => {
  assert.equal(playbackProgress(15, 60), 25)
  assert.equal(playbackProgress(-5, 60), 0)
  assert.equal(playbackProgress(80, 60), 100)
  assert.equal(playbackProgress(10, 0), 0)
})
