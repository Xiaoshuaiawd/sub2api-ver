import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const { getOAuthCostSummary, getOAuthCost, updateOAuthCost } = vi.hoisted(() => ({
  getOAuthCostSummary: vi.fn(),
  getOAuthCost: vi.fn(),
  updateOAuthCost: vi.fn(),
}))

vi.mock('@/api/admin/usage', () => ({
  getOAuthCostSummary,
  adminUsageAPI: {
    getOAuthCostSummary,
  },
}))

vi.mock('@/api/admin/settings', () => ({
  getOAuthCost,
  updateOAuthCost,
  settingsAPI: {
    getOAuthCost,
    updateOAuthCost,
  },
}))

const messages: Record<string, string> = {
  'usage.oauthCost.title': 'OAuth Cost Accounting',
  'usage.oauthCost.description': 'Actual unit cost for purchased OAuth accounts',
  'usage.oauthCost.purchaseCost': 'Purchase cost',
  'usage.oauthCost.consumedQuota': 'Total consumed quota',
  'usage.oauthCost.unitCost': 'Cost per quota',
  'usage.oauthCost.unitCostUnavailable': 'No consumed quota yet',
  'usage.oauthCost.accountCount': 'OAuth accounts',
  'usage.oauthCost.requests': 'Requests',
  'usage.oauthCost.tokens': 'Tokens',
  'usage.oauthCost.save': 'Save',
  'usage.oauthCost.saving': 'Saving...',
  'usage.oauthCost.saved': 'Saved',
  'usage.oauthCost.saveFailed': 'Save failed',
  'common.loading': 'Loading...',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

import OAuthCostSummaryCard from '../OAuthCostSummaryCard.vue'

describe('OAuthCostSummaryCard', () => {
  beforeEach(() => {
    getOAuthCostSummary.mockReset()
    getOAuthCost.mockReset()
    updateOAuthCost.mockReset()
  })

  it('calculates unit cost as purchase_cost_cny divided by total_consumed_quota', async () => {
    getOAuthCostSummary.mockResolvedValue({
      account_count: 3,
      total_consumed_quota: 250,
      total_requests: 7,
      total_tokens: 900,
    })
    getOAuthCost.mockResolvedValue({ purchase_cost_cny: 50 })

    const wrapper = mount(OAuthCostSummaryCard, {
      global: {
        stubs: {
          Icon: true,
        },
      },
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="oauth-total-consumed-quota"]').text()).toContain('250.000000')
    expect(wrapper.get('[data-testid="oauth-unit-cost"]').text()).toContain('0.200000')
    expect(wrapper.text()).toContain('OAuth accounts')
    expect(wrapper.text()).toContain('3')
  })

  it('shows an empty state instead of an invalid unit cost when consumed quota is zero', async () => {
    getOAuthCostSummary.mockResolvedValue({
      account_count: 1,
      total_consumed_quota: 0,
      total_requests: 0,
      total_tokens: 0,
    })
    getOAuthCost.mockResolvedValue({ purchase_cost_cny: 50 })

    const wrapper = mount(OAuthCostSummaryCard, {
      global: {
        stubs: {
          Icon: true,
        },
      },
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="oauth-unit-cost"]').text()).toContain('No consumed quota yet')
    expect(wrapper.text()).not.toContain('Infinity')
    expect(wrapper.text()).not.toContain('NaN')
  })
})
