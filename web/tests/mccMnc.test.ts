import assert from 'node:assert/strict'
import test from 'node:test'
import {
  buildMccMncIndex,
  lookupMccMncRow,
  mccMncCountryCode
} from '../src/utils/mcc-mnc.ts'

test('falls back from any PLMN to its backend-provided MCC country row', () => {
  const countries = [
    { mcc: '234', plmn: '23410', iso: 'gb', country: 'United Kingdom' },
    { mcc: '460', plmn: '46000', iso: 'cn', country: 'China' },
    { mcc: '310', plmn: '310260', iso: 'us', country: 'United States' },
    { mcc: '262', plmn: '26202', iso: 'de', country: 'Germany' }
  ]
  const index = buildMccMncIndex(countries.map((entry) => ({
    mcc: entry.mcc,
    mnc: '',
    iso: entry.iso,
    country: entry.country,
    country_code: entry.iso,
    network: ''
  })))

  for (const entry of countries) {
    assert.equal(lookupMccMncRow(index, entry.plmn)?.country, entry.country)
    assert.equal(mccMncCountryCode(index, entry.plmn), entry.iso)
  }
})

test('prefers an exact operator row over the MCC country fallback', () => {
  const index = buildMccMncIndex([
    {
      mcc: '234',
      mnc: '',
      iso: 'gb',
      country: 'United Kingdom',
      country_code: 'gb',
      network: ''
    },
    {
      mcc: '234',
      mnc: '10',
      iso: 'gb',
      country: 'United Kingdom',
      country_code: 'gb',
      network: 'O2 UK'
    }
  ])

  assert.equal(lookupMccMncRow(index, '23410')?.network, 'O2 UK')
})

test('maps legacy MCC table country codes to available ISO flag codes', () => {
  const legacyRows = [
    { mcc: '362', legacy: 'an', current: 'cw' },
    { mcc: '340', legacy: 'fg', current: 'gf' },
    { mcc: '514', legacy: 'tp', current: 'tl' }
  ]
  const index = buildMccMncIndex(legacyRows.map((entry) => ({
    mcc: entry.mcc,
    mnc: '',
    iso: entry.legacy,
    country: '',
    country_code: entry.legacy,
    network: ''
  })))

  for (const entry of legacyRows) {
    assert.equal(mccMncCountryCode(index, entry.mcc), entry.current)
  }
})
