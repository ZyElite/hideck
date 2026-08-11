import assert from 'node:assert/strict'
import test from 'node:test'
import type { CommandDefinition } from '../src/types/commands'
import { buildDangerousCommand, carrierReplySenderError, commandSuggestions, commandTemplate } from '../src/utils/commandInput'

const definitions: CommandDefinition[] = [
  { name: 'send', usage: '/send [设备]', summary: '发送', dangerous: false, async: true, device_argument: true },
  { name: 'sms', usage: '/sms [设备]', summary: '短信', dangerous: false, async: false, device_argument: true },
  { name: 'list', usage: '/list', summary: '列表', dangerous: false, async: false, device_argument: false }
]

test('filters slash command suggestions before the first argument', () => {
  assert.deepEqual(commandSuggestions('/s', definitions).map((item) => item.name), ['send', 'sms'])
  assert.deepEqual(commandSuggestions('/s wwan0', definitions), [])
  assert.deepEqual(commandSuggestions('s', definitions), [])
})

test('creates a command template that is ready for required arguments', () => {
  assert.equal(commandTemplate(definitions[0]), '/send ')
  assert.equal(commandTemplate(definitions[0], 'wwan0'), '/send wwan0')
  assert.equal(commandTemplate(definitions[2]), '/list')
})

test('requires a sender only for SMS reply rules', () => {
  assert.match(carrierReplySenderError('sms', '  \n'), /预期发送者/)
  assert.equal(carrierReplySenderError('sms', '85075'), '')
  assert.equal(carrierReplySenderError('direct', ''), '')
})

test('builds dangerous quick actions only with complete explicit arguments', () => {
  assert.equal(buildDangerousCommand({ name: 'rotate', device: 'wwan0' }), '/rotate wwan0')
  assert.equal(buildDangerousCommand({ name: 'switch', device: 'wwan0', target: '2' }), '/switch wwan0 2')
  assert.equal(buildDangerousCommand({ name: 'vocall', device: 'wwan0', phone: '888', duration: 15 }), '/vocall wwan0 888 15')
  assert.throws(() => buildDangerousCommand({ name: 'switch', device: 'wwan0' }), /Profile/)
})
