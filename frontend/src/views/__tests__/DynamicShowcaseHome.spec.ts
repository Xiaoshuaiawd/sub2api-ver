import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, RouterLinkStub } from '@vue/test-utils'
import DynamicShowcaseHome from '../DynamicShowcaseHome.vue'

const { getHomeCatalog, appStore, authStore } = vi.hoisted(() => ({
  getHomeCatalog: vi.fn(),
  appStore: {
    cachedPublicSettings: { site_name: 'Test Gateway', site_subtitle: 'Test subtitle' },
    siteName: 'Fallback', siteLogo: '', docUrl: '',
  },
  authStore: { isAuthenticated: false, isAdmin: false },
}))

vi.mock('@/api/homeCatalog', () => ({ getHomeCatalog }))
vi.mock('@/stores', () => ({ useAppStore: () => appStore, useAuthStore: () => authStore }))
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return { ...actual, useI18n: () => ({ t: (key: string, params?: { count?: number }) => params?.count === undefined ? key : `${key} ${params.count}` }) }
})

function mountShowcase() {
  return mount(DynamicShowcaseHome, {
    global: { stubs: {
      RouterLink: RouterLinkStub,
      LocaleSwitcher: { template: '<div />' },
      Icon: { template: '<span />' },
    } },
  })
}

describe('DynamicShowcaseHome', () => {
  beforeEach(() => {
    getHomeCatalog.mockReset()
    authStore.isAuthenticated = false
    authStore.isAdmin = false
  })

  it('loads active catalog, deduplicates models, and filters groups by model name', async () => {
    getHomeCatalog.mockResolvedValue([
      { id: 1, name: 'Claude', description: 'Coding', platform: 'anthropic', subscription_type: 'subscription_balance', is_exclusive: false, models: [{ name: 'claude-sonnet', platform: 'anthropic' }] },
      { id: 2, name: 'Second', description: '', platform: 'anthropic', subscription_type: 'standard', is_exclusive: false, models: [{ name: 'claude-sonnet', platform: 'anthropic' }] },
      { id: 3, name: 'Empty', description: '', platform: 'openai', subscription_type: 'subscription', is_exclusive: false, models: [] },
    ])
    const wrapper = mountShowcase()
    await flushPromises()

    expect(getHomeCatalog).toHaveBeenCalledOnce()
    expect(wrapper.findAll('#groups article')).toHaveLength(3)
    expect(wrapper.findAll('#models .truncate.text-sm.font-semibold')).toHaveLength(1)
    await wrapper.get('#showcase-search').setValue('sonnet')
    expect(wrapper.findAll('#groups article')).toHaveLength(2)
    expect(wrapper.text()).toContain('showcase.billingHybrid')
  })

  it('offers a retry when the catalog request fails', async () => {
    getHomeCatalog.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce([])
    const wrapper = mountShowcase()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('showcase.loadError')
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('showcase.noGroups')
  })
})
