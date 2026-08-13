import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { prepareManualBalanceInput } from '../src/utils/manualBalance'
import { createDeviceRequestScope } from '../src/utils/deviceRequestScope'

const service = await readFile(new URL('../src/services/commands.ts', import.meta.url), 'utf8')
const commands = await readFile(new URL('../src/views/Commands.vue', import.meta.url), 'utf8')
const panel = await readFile(new URL('../src/components/commands/BalancePanel.vue', import.meta.url), 'utf8')
const dialog = await readFile(new URL('../src/components/commands/ManualBalanceDialog.vue', import.meta.url), 'utf8')
const timeline = await readFile(new URL('../src/components/commands/CommandTimeline.vue', import.meta.url), 'utf8')
const balanceMessage = await readFile(new URL('../src/components/commands/BalanceMessage.vue', import.meta.url), 'utf8')

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

test('manual balance remains independently addressable and visually distinct', () => {
  assert.match(service, /api\.get\(`\/devices\/\$\{encodeURIComponent\(deviceId\)\}\/manual-balance`\)/)
  assert.match(commands, /const displayedBalances = computed/)
  assert.match(commands, /loadManualBalance\(true\)/)
  assert.match(timeline, /tone === 'manual'/)
  assert.match(timeline, /\.tone-manual \{ color: var\(--ui-primary\); \}/)
  assert.match(balanceMessage, /\.tone-manual \{ color: var\(--ui-primary\); \}/)
})

test('manual balance mutations invalidate stale reads and remain the current display value', () => {
  const scope = createDeviceRequestScope('wwan0')
  const staleRead = scope.begin('wwan0')
  scope.invalidate('wwan0')
  assert.equal(scope.isCurrent(staleRead, 'wwan0'), false)

  assert.match(commands, /manualBalanceRequestScope\.invalidate\(selectedDevice\.value\)[\s\S]*balances\.value = \[result\.data/)
  assert.match(commands, /manualBalanceRequestScope\.invalidate\(selectedDevice\.value\)[\s\S]*balances\.value = balances\.value\.filter/)
  assert.match(panel, /const latestQuery = computed\(\(\) => manualQuery\.value \|\| selectedQueries\.value\[0\]\)/)
})

test('manual balance mutations stay bound to the device that opened the dialog', () => {
  assert.match(commands, /const operationDeviceID = manualBalanceDeviceID\.value/g)
  assert.match(commands, /setManualBalance\(operationDeviceID, input\)/)
  assert.match(commands, /clearManualBalance\(operationDeviceID\)/)
  assert.match(commands, /item\.device_id !== operationDeviceID/)
  assert.match(commands, /if \(selectedDevice\.value === operationDeviceID\) \{[\s\S]*manualBalanceRequestScope\.invalidate\(operationDeviceID\)/)
  assert.match(commands, /manualBalanceDeviceID\.value !== operationDeviceID/)
  assert.match(commands, /:device="manualBalanceDevice"/)
  assert.doesNotMatch(commands, /clearManualBalance\(selectedDevice\.value\)/)
})

test('manual balance dialog opens only after its device value is loaded', () => {
  assert.match(commands, /const loaded = await loadManualBalance\(true, operationDeviceID\)/)
  assert.match(commands, /if \(!loaded \|\| selectedDevice\.value !== operationDeviceID\) return/)
  assert.match(commands, /manualBalanceDialogExisting\.value = manualBalance\.value/)
  assert.match(commands, /:manual-balance-opening="manualBalanceOpening"/)
  assert.match(panel, /:loading="manualBalanceOpening"/)
  assert.match(panel, /querying \|\| manualBalanceOpening/)
})
