import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { prepareManualBalanceInput } from '../src/utils/manualBalance'

const service = await readFile(new URL('../src/services/commands.ts', import.meta.url), 'utf8')
const commands = await readFile(new URL('../src/views/Commands.vue', import.meta.url), 'utf8')
const panel = await readFile(new URL('../src/components/commands/BalancePanel.vue', import.meta.url), 'utf8')
const dialog = await readFile(new URL('../src/components/commands/ManualBalanceDialog.vue', import.meta.url), 'utf8')

test('manual balance input normalizes amount and currency without inventing defaults', () => {
  assert.deepEqual(prepareManualBalanceInput(' 12,89 ', ' gbp '), { amount: '12.89', currency: 'GBP' })
  assert.deepEqual(prepareManualBalanceInput('-1', ''), { amount: '-1', currency: '' })
  assert.throws(() => prepareManualBalanceInput('unknown', 'GBP'), /金额必须是数字/)
})

test('manual balance UI uses real save and clear endpoints and labels its source', () => {
  assert.match(service, /api\.put\(`\/devices\/\$\{encodeURIComponent\(deviceId\)\}\/manual-balance`, input\)/)
  assert.match(service, /api\.delete\(`\/devices\/\$\{encodeURIComponent\(deviceId\)\}\/manual-balance`\)/)
  assert.match(commands, /commandService\.setManualBalance/)
  assert.match(commands, /commandService\.clearManualBalance/)
  assert.match(panel, /编辑手动余额/)
  assert.match(dialog, /来源会明确显示为“手动录入”/)
  assert.match(dialog, /清除手动值/)
})
