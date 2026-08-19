import { describe, expect, it } from 'vitest'

import { buildProModeUsage } from '../proMode'

describe('buildProModeUsage', () => {
  it('returns stable usage and cost for the same account window', () => {
    const first = buildProModeUsage(42, '5h')
    const second = buildProModeUsage(42, '5h')

    expect(second).toEqual(first)
    expect(second.requests).toBe(first.requests)
  })

  it('keeps utilization and per-percent cost within the configured ranges', () => {
    for (let accountId = 1; accountId <= 200; accountId += 1) {
      const fiveHour = buildProModeUsage(accountId, '5h')
      const usage = buildProModeUsage(accountId, '7d')

      expect(fiveHour.utilization).toBeGreaterThanOrEqual(0)
      expect(fiveHour.utilization).toBeLessThanOrEqual(3)
      expect(fiveHour.resetAfterHours).toBeNull()
      expect(usage.utilization).toBeGreaterThanOrEqual(30)
      expect(usage.utilization).toBeLessThanOrEqual(60)
      expect(usage.resetAfterHours).toBeGreaterThanOrEqual(158)
      expect(usage.resetAfterHours).toBeLessThanOrEqual(166)
      expect(usage.costPerPercent).toBeGreaterThanOrEqual(20)
      expect(usage.costPerPercent).toBeLessThanOrEqual(26)
      expect(usage.cost).toBeCloseTo(usage.utilization * usage.costPerPercent, 2)
      expect(usage.requests).toBeGreaterThanOrEqual(1000)
      expect(usage.requests).toBeLessThanOrEqual(2500)
    }
  })

  it('uses the account and window as independent random seeds', () => {
    const samples = new Set<string>()

    for (let accountId = 1; accountId <= 20; accountId += 1) {
      const usage = buildProModeUsage(accountId, '7d')
      samples.add(`${usage.utilization}:${usage.costPerPercent}`)
    }

    expect(samples.size).toBeGreaterThan(10)
  })
})
