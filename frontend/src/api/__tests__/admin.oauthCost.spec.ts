import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, put } = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    put,
  },
}))

import { getOAuthCostSummary } from '@/api/admin/usage'
import { getOAuthCost, updateOAuthCost } from '@/api/admin/settings'

describe('admin OAuth cost APIs', () => {
  beforeEach(() => {
    get.mockReset()
    put.mockReset()
  })

  it('loads OAuth cost summary from the admin usage endpoint', async () => {
    const summary = {
      account_count: 2,
      total_consumed_quota: 23,
      total_requests: 4,
      total_tokens: 99,
    }
    get.mockResolvedValue({ data: summary })

    await expect(getOAuthCostSummary()).resolves.toEqual(summary)

    expect(get).toHaveBeenCalledWith('/admin/usage/oauth-cost-summary')
  })

  it('loads and saves the OAuth purchase cost setting', async () => {
    get.mockResolvedValue({ data: { purchase_cost_cny: 50 } })
    put.mockResolvedValue({ data: { purchase_cost_cny: 88.5 } })

    await expect(getOAuthCost()).resolves.toEqual({ purchase_cost_cny: 50 })
    await expect(updateOAuthCost({ purchase_cost_cny: 88.5 })).resolves.toEqual({ purchase_cost_cny: 88.5 })

    expect(get).toHaveBeenCalledWith('/admin/settings/oauth-cost')
    expect(put).toHaveBeenCalledWith('/admin/settings/oauth-cost', { purchase_cost_cny: 88.5 })
  })
})
