import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const overviewTab = await readFile(
  new URL('../src/components/DeviceOverviewTab.vue', import.meta.url),
  'utf8'
)
const connectionStage = await readFile(
  new URL('../src/components/DeviceOverviewConnectionStage.vue', import.meta.url),
  'utf8'
)
const identityPanel = await readFile(
  new URL('../src/components/DeviceOverviewIdentityPanel.vue', import.meta.url),
  'utf8'
)

test('VoWiFi overview exposes one service path and centralizes diagnostics', () => {
  assert.doesNotMatch(overviewTab, /VoWiFi 运行时|readinessItems|showVowifiDetail/)
  assert.match(overviewTab, /v-if="!device\?\.vowifi_enabled"/)
  assert.match(overviewTab, /<DeviceOverviewIdentityPanel/)
  assert.equal(connectionStage.match(/class="overview-service-path"/g)?.length, 1)

  for (const label of ['接入方式', '数据平面', '协议', '接口', '最后原因', '错误分类']) {
    assert.match(connectionStage, new RegExp(`label: '${label}'`))
  }
})

test('identity panel keeps production facts and existing operations', () => {
  for (const label of [
    'IMEI',
    'ICCID',
    'IMSI',
    '本机号码',
    '原运营商',
    '固件版本',
    '飞行模式',
    '运行模式'
  ]) {
    assert.match(identityPanel, new RegExp(`label: '${label}'`))
  }

  assert.match(identityPanel, /useSensitiveVisibility/)
  assert.match(identityPanel, /copyToClipboard/)
  assert.match(identityPanel, /device\?\.e911_setup_available/)
  assert.match(identityPanel, /emit\('setup-e911'\)/)
})

test('cellular signal panel hides invalid modem sentinel values', () => {
  assert.match(overviewTab, /dbm > QMI_INVALID_SIGNAL_DBM/)
  assert.match(overviewTab, /signalDbmDisplay/)
  assert.match(overviewTab, /signalMetricDisplay/)
  assert.doesNotMatch(overviewTab, /device\?\.modem\?\.signal_dbm \?\? '--'/)
})
