import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

const oauthCostKeys = [
  'usage.oauthCost.title',
  'usage.oauthCost.description',
  'usage.oauthCost.purchaseCost',
  'usage.oauthCost.consumedQuota',
  'usage.oauthCost.unitCost',
  'usage.oauthCost.unitCostUnavailable',
  'usage.oauthCost.accountCount',
  'usage.oauthCost.requests',
  'usage.oauthCost.tokens',
  'usage.oauthCost.save',
  'usage.oauthCost.saving',
  'usage.oauthCost.saved',
  'usage.oauthCost.saveFailed',
  'usage.oauthCost.invalidPurchaseCost',
  'usage.oauthCost.loadFailed',
  'usage.oauthCost.retry',
] as const

function readPath(messages: Record<string, unknown>, key: string): unknown {
  return key.split('.').reduce<unknown>((current, part) => {
    if (current && typeof current === 'object' && part in current) {
      return (current as Record<string, unknown>)[part]
    }
    return undefined
  }, messages)
}

describe('oauth cost locale keys', () => {
  it.each(oauthCostKeys)('defines %s in both supported locales', (key) => {
    expect(readPath(en, key), `missing English key: ${key}`).toEqual(expect.any(String))
    expect(readPath(zh, key), `missing Chinese key: ${key}`).toEqual(expect.any(String))
  })

  it.each(oauthCostKeys)('keeps %s localized in Chinese', (key) => {
    expect(readPath(zh, key)).not.toBe(readPath(en, key))
  })
})
