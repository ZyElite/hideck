import type { ManualBalanceInput } from '../types/commands'

const MANUAL_AMOUNT_PATTERN = /^-?[0-9]+(?:[.,][0-9]+)?$/
const MAX_AMOUNT_LENGTH = 64
const MAX_CURRENCY_LENGTH = 12

export function prepareManualBalanceInput(amount: string, currency: string): ManualBalanceInput {
  const trimmedAmount = amount.trim()
  if (!trimmedAmount || trimmedAmount.length > MAX_AMOUNT_LENGTH || !MANUAL_AMOUNT_PATTERN.test(trimmedAmount)) {
    throw new Error('金额必须是数字')
  }
  const trimmedCurrency = currency.trim().toUpperCase()
  if (trimmedCurrency.length > MAX_CURRENCY_LENGTH) {
    throw new Error(`币种不能超过 ${MAX_CURRENCY_LENGTH} 个字符`)
  }
  return { amount: trimmedAmount.replaceAll(',', '.'), currency: trimmedCurrency }
}
