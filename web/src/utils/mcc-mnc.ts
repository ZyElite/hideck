export type MccMncRow = {
  mcc: string
  mnc: string
  iso: string
  country: string
  country_code: string
  network: string
}

export type ServingOperatorLike = {
  operator?: string
  mcc?: string
  mnc?: string
}

type CountryGroup = {
  country_code?: string
  country_name?: string
  mccs?: string[]
}

const TABLE_URL = 'https://raw.githubusercontent.com/musalbas/mcc-mnc-table/refs/heads/master/mcc-mnc-table.json'
const STORAGE_KEY = 'go-4gproxy:mcc-mnc-table:v1'
const CACHE_TTL_MS = 7 * 24 * 60 * 60 * 1000
const LEGACY_COUNTRY_CODE_ALIASES: Readonly<Record<string, string>> = Object.freeze({
  an: 'cw', // Netherlands Antilles MCC 362 is now represented by Curacao.
  fg: 'gf', // French Guiana's ISO 3166-1 code is GF.
  tp: 'tl' // East Timor was renamed Timor-Leste and reassigned from TP to TL.
})

type CachePayload = {
  fetched_at: number
  rows: MccMncRow[]
}

let indexPromise: Promise<Map<string, MccMncRow>> | null = null

function isAllDigits(s: string): boolean {
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i)
    if (c < 48 || c > 57) return false
  }
  return s.length > 0
}

function normalizeCode(s: unknown): string {
  return String(s || '').trim()
}

function normalizeCountryCode(value: unknown): string {
  const code = normalizeCode(value).toLowerCase()
  return LEGACY_COUNTRY_CODE_ALIASES[code] || code
}

export function buildMccMncIndex(rows: MccMncRow[]): Map<string, MccMncRow> {
  const idx = new Map<string, MccMncRow>()
  for (const r of rows) {
    const mcc = normalizeCode(r?.mcc)
    const mnc = normalizeCode(r?.mnc)
    if (!mcc) continue
    const key = `${mcc}${mnc}`
    if (!idx.has(key)) {
      idx.set(key, {
        mcc,
        mnc,
        iso: normalizeCountryCode(r?.iso),
        country: normalizeCode(r?.country),
        country_code: normalizeCountryCode(r?.country_code),
        network: normalizeCode(r?.network)
      })
      continue
    }
    const cur = idx.get(key)!
    if (!cur.network && r?.network) {
      cur.network = normalizeCode(r.network)
    }
  }
  return idx
}

function countryGroupRows(groups: CountryGroup[]): MccMncRow[] {
  const rows: MccMncRow[] = []
  for (const group of groups) {
    const countryCode = normalizeCountryCode(group?.country_code)
    if (countryCode.length !== 2 || !Array.isArray(group?.mccs)) continue
    for (const rawMCC of group.mccs) {
      const mcc = normalizeCode(rawMCC)
      if (mcc.length !== 3 || !isAllDigits(mcc)) continue
      rows.push({
        mcc,
        mnc: '',
        iso: countryCode,
        country: normalizeCode(group.country_name),
        country_code: countryCode,
        network: ''
      })
    }
  }
  return rows
}

async function fetchBackendCountryRows(): Promise<MccMncRow[]> {
  const { api } = await import('../stores/auth')
  const response = await api.get('/upstream-proxy-countries')
  return countryGroupRows(Array.isArray(response.data) ? response.data : [])
}

function readCache(): CachePayload | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const data = JSON.parse(raw) as CachePayload
    if (!data || !Array.isArray(data.rows) || typeof data.fetched_at !== 'number') return null
    return data
  } catch {
    return null
  }
}

function writeCache(rows: MccMncRow[]) {
  try {
    const payload: CachePayload = { fetched_at: Date.now(), rows }
    localStorage.setItem(STORAGE_KEY, JSON.stringify(payload))
  } catch {
    // Ignore cache write failures (private mode/quota/security policy).
  }
}

async function fetchRows(): Promise<MccMncRow[]> {
  const res = await fetch(TABLE_URL, { method: 'GET' })
  if (!res.ok) throw new Error(`mcc-mnc-table fetch failed: ${res.status}`)
  const data = await res.json()
  if (!Array.isArray(data)) return []
  const out: MccMncRow[] = []
  for (const it of data) {
    if (!it || typeof it !== 'object') continue
    const r = it as Record<string, unknown>
    const mcc = typeof r.mcc === 'string' ? r.mcc : ''
    const mnc = typeof r.mnc === 'string' ? r.mnc : ''
    if (!mcc || !mnc) continue
    out.push({
      mcc,
      mnc,
      iso: typeof r.iso === 'string' ? r.iso : '',
      country: typeof r.country === 'string' ? r.country : '',
      country_code: typeof r.country_code === 'string' ? r.country_code : '',
      network: typeof r.network === 'string' ? r.network : ''
    })
  }
  return out
}

export async function getMccMncIndex(): Promise<Map<string, MccMncRow>> {
  if (indexPromise) return indexPromise
  indexPromise = (async () => {
    let countryRows: MccMncRow[] = []
    try {
      countryRows = await fetchBackendCountryRows()
    } catch (error) {
      console.warn('设备本地 MCC 国家表读取失败', error)
    }
    const cache = readCache()
    const now = Date.now()
    if (cache && cache.rows.length > 0 && now - cache.fetched_at < CACHE_TTL_MS) {
      return buildMccMncIndex([...countryRows, ...cache.rows])
    }
    try {
      const rows = await fetchRows()
      if (rows.length > 0) writeCache(rows)
      return buildMccMncIndex([...countryRows, ...rows])
    } catch (error) {
      console.warn('外部 MCC/MNC 运营商表读取失败', error)
      if (cache && cache.rows.length > 0) {
        return buildMccMncIndex([...countryRows, ...cache.rows])
      }
      return buildMccMncIndex(countryRows)
    }
  })()
  return indexPromise
}

export function lookupMccMncRow(index: Map<string, MccMncRow> | null, code: string): MccMncRow | null {
  if (!index) return null
  const normalized = normalizeCode(code)
  if (!normalized) return null
  return index.get(normalized) || index.get(normalized.slice(0, 3)) || null
}

export function mccMncCountryCode(index: Map<string, MccMncRow> | null, code: string): string {
  const row = lookupMccMncRow(index, code)
  return normalizeCountryCode(row?.iso || row?.country_code)
}

export function isoToFlagEmoji(iso: string): string {
  const s = normalizeCode(iso).toUpperCase()
  if (s.length !== 2) return ''
  const a = s.charCodeAt(0)
  const b = s.charCodeAt(1)
  if (a < 65 || a > 90 || b < 65 || b > 90) return ''
  return String.fromCodePoint(0x1f1e6 + (a - 65)) + String.fromCodePoint(0x1f1e6 + (b - 65))
}

export function getMncCandidateLengths(mcc: string): number[] {
  const m3 = [
    '302', '308', // 加拿大等
    '310', '311', '312', '313', '314', '315', '316', '332', '318', '319', '334', '350', // 美国及属地
    '338', '348', '342', '344', '346', '354', '356', '358', '360', '362', '364', '365', '366', '368', '370', '372', '374', '376',
    '405', '406', // 印度 (部分)
    '716', '722', '730', '732', '736', '740', '744', '746', '748', '750', // 南美洲
  ]
  if (m3.includes(mcc)) return [3]
  return [2, 3] // 其他地区默认优先 2位, 然后是3位
}

export function lookupServingOperatorNameFromPLMN(index: Map<string, MccMncRow>, modem: ServingOperatorLike): MccMncRow | null {
  const op = normalizeCode(modem?.operator || '')
  if (op && (op.length === 5 || op.length === 6) && isAllDigits(op)) {
    const hit = lookupMccMncRow(index, op)
    if (hit) return hit
  }

  const mcc = normalizeCode(modem?.mcc || '')
  const mnc = normalizeCode(modem?.mnc || '')
  if (mcc && mnc) {
    const hit = lookupMccMncRow(index, `${mcc}${mnc}`)
    if (hit) return hit
  }

  return null
}

export function formatServingOperatorDisplay(modem: ServingOperatorLike, index: Map<string, MccMncRow> | null): string {
  const op = normalizeCode(modem?.operator || '')
  if (!index) return op || '--'
  const row = lookupServingOperatorNameFromPLMN(index, modem)
  if (!row) return op || '--'
  const flag = isoToFlagEmoji(row.iso)
  const name = normalizeCode(row.network) || normalizeCode(row.country) || '--'
  const code = `${normalizeCode(row.mcc)}${normalizeCode(row.mnc)}`
  return `${flag ? flag + ' ' : ''}${name}${code ? ` (${code})` : ''}`
}
