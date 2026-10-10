import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import DashboardView from '../DashboardView.vue'

const { statsMock, trendMock, modelsMock, recentMock, quotasMock, refreshUserMock } = vi.hoisted(() => ({
  statsMock: vi.fn(), trendMock: vi.fn(), modelsMock: vi.fn(), recentMock: vi.fn(), quotasMock: vi.fn(), refreshUserMock: vi.fn(),
}))

vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { balance: 12 }, isSimpleMode: false, refreshUser: refreshUserMock }) }))
vi.mock('@/api/usage', () => ({ usageAPI: {
  getDashboardStats: statsMock, getDashboardTrend: trendMock, getDashboardModels: modelsMock, getByDateRange: recentMock,
} }))
vi.mock('@/api/user', () => ({ getMyPlatformQuotas: quotasMock }))
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return { ...actual, useI18n: () => ({ t: (key: string) => key, locale: ref('zh-CN') }) }
})

function mountDashboard() {
  return mount(DashboardView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    UserDashboardStats: { template: '<div data-testid="stats" />' },
    UserDashboardCharts: { template: '<div data-testid="charts" />' },
    UserDashboardRecentUsage: { template: '<div data-testid="recent" />' },
    UserDashboardQuickActions: { template: '<div data-testid="actions" />' },
    LoadingSpinner: true, Icon: true,
  } } })
}

describe('user DashboardView', () => {
  beforeEach(() => {
    statsMock.mockReset().mockResolvedValue({ today_requests: 4 })
    trendMock.mockReset().mockResolvedValue({ trend: [] })
    modelsMock.mockReset().mockResolvedValue({ models: [] })
    recentMock.mockReset().mockResolvedValue({ items: [] })
    quotasMock.mockReset().mockResolvedValue({ platform_quotas: [] })
    refreshUserMock.mockReset().mockResolvedValue({ balance: 12 })
  })

  it('shows account, trend, and recent overview after loading', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.text()).toContain('dashboard.todayTitle')
    expect(wrapper.get('[data-testid="stats"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="charts"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="recent"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="actions"]').exists()).toBe(true)
  })

  it('offers a retry when the overview cannot load', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    statsMock.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ today_requests: 4 })
    try {
      const wrapper = mountDashboard()
      await flushPromises()
      expect(wrapper.text()).toContain('dashboard.loadFailed')
      await wrapper.get('[data-testid="dashboard-retry"]').trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-testid="stats"]').exists()).toBe(true)
    } finally {
      consoleError.mockRestore()
    }
  })
})
