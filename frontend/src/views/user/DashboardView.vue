<template>
  <AppLayout variant="dashboard">
    <div class="dashboard-page -m-4 min-h-screen px-4 py-8 md:-m-6 md:px-6 lg:-m-8 lg:px-8 lg:py-10">
      <div class="mx-auto max-w-[1320px] space-y-12">
        <header class="dashboard-heading flex flex-wrap items-end justify-between gap-5">
          <div>
            <h1 class="text-3xl font-bold tracking-tight text-slate-950 dark:text-white sm:text-4xl">{{ t('dashboard.todayTitle') }}</h1>
          </div>
          <div class="flex items-center gap-3">
            <span class="hidden text-sm text-slate-500 dark:text-slate-400 sm:inline">{{ dateLabel }}</span>
            <button type="button" class="dashboard-refresh inline-flex min-h-11 items-center gap-2 rounded-full border border-slate-200 bg-white px-4 py-2 text-sm font-medium text-slate-700 shadow-sm transition dark:border-dark-700 dark:bg-dark-800 dark:text-slate-200" :disabled="refreshing" @click="refreshAll">
              <Icon name="refresh" size="sm" :class="{ 'animate-spin': refreshing }" />
              {{ t('common.refresh') }}
            </button>
          </div>
        </header>

        <div v-if="loading && !stats" class="flex min-h-80 items-center justify-center"><LoadingSpinner /></div>
        <template v-else-if="stats">
          <section class="dashboard-section dashboard-appear">
            <div class="dashboard-section-heading"><h2>{{ t('dashboard.accountOverview') }}</h2></div>
            <div class="dashboard-stats space-y-5">
              <UserDashboardStats :stats="stats" :balance="user?.balance || 0" :is-simple="authStore.isSimpleMode" :platform-quotas="platformQuotas" />
            </div>
          </section>

          <section class="dashboard-section dashboard-appear" style="animation-delay: 80ms">
            <div class="dashboard-section-heading"><h2>{{ t('dashboard.trendsTitle') }}</h2></div>
            <UserDashboardCharts v-model:startDate="startDate" v-model:endDate="endDate" v-model:granularity="granularity" :loading="loadingCharts" :trend="trendData" :models="modelStats" @dateRangeChange="loadCharts" @granularityChange="loadCharts" @refresh="refreshAll" />
          </section>

          <section class="dashboard-section dashboard-appear" style="animation-delay: 160ms">
            <div class="dashboard-section-heading"><h2>{{ t('dashboard.recentOverview') }}</h2></div>
            <div class="grid grid-cols-1 gap-5 lg:grid-cols-3">
              <div class="lg:col-span-2"><UserDashboardRecentUsage :data="recentUsage" :loading="loadingUsage" /></div>
              <div><UserDashboardQuickActions /></div>
            </div>
          </section>
        </template>
        <div v-else class="rounded-2xl border border-slate-200 bg-white px-6 py-16 text-center dark:border-dark-700 dark:bg-dark-800">
          <p class="text-sm text-slate-600 dark:text-slate-300">{{ t('dashboard.loadFailed') }}</p>
          <button type="button" data-testid="dashboard-retry" class="dashboard-refresh mt-5 min-h-11 rounded-full border border-indigo-200 px-5 py-2 text-sm font-medium text-indigo-700 dark:border-indigo-800 dark:text-indigo-300" @click="refreshAll">{{ t('common.tryAgain') }}</button>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { usageAPI, type UserDashboardStats as UserStatsType } from '@/api/usage'
import { getMyPlatformQuotas } from '@/api/user'
import { formatDateLocalInput } from '@/utils/format'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'
import UserDashboardCharts from '@/components/user/dashboard/UserDashboardCharts.vue'
import UserDashboardRecentUsage from '@/components/user/dashboard/UserDashboardRecentUsage.vue'
import UserDashboardQuickActions from '@/components/user/dashboard/UserDashboardQuickActions.vue'
import type { UsageLog, TrendDataPoint, ModelStat, PlatformQuotaItem } from '@/types'

const { t, locale } = useI18n()
const authStore = useAuthStore()
const user = computed(() => authStore.user)
const stats = ref<UserStatsType | null>(null)
const loading = ref(false)
const loadingUsage = ref(false)
const loadingCharts = ref(false)
const trendData = ref<TrendDataPoint[]>([])
const modelStats = ref<ModelStat[]>([])
const recentUsage = ref<UsageLog[]>([])
const platformQuotas = ref<PlatformQuotaItem[] | null>(null)
const startDate = ref(formatDateLocalInput(new Date(Date.now() - 6 * 86400000)))
const endDate = ref(formatDateLocalInput(new Date()))
const granularity = ref('day')
const refreshing = computed(() => loading.value || loadingUsage.value || loadingCharts.value)
const dateLabel = computed(() => new Intl.DateTimeFormat(locale.value.startsWith('zh') ? 'zh-CN' : 'en-US', { month: 'long', day: 'numeric', weekday: 'long' }).format(new Date()))

async function loadStats() {
  loading.value = true
  try {
    await authStore.refreshUser()
    stats.value = await usageAPI.getDashboardStats()
  } catch (error) {
    console.error('Failed to load dashboard stats:', error)
  } finally {
    loading.value = false
  }
}
async function loadCharts() {
  loadingCharts.value = true
  try {
    const res = await Promise.all([
      usageAPI.getDashboardTrend({ start_date: startDate.value, end_date: endDate.value, granularity: granularity.value as 'day' | 'hour' }),
      usageAPI.getDashboardModels({ start_date: startDate.value, end_date: endDate.value })
    ])
    trendData.value = res[0].trend || []
    modelStats.value = res[1].models || []
  } catch (error) {
    console.error('Failed to load charts:', error)
  } finally {
    loadingCharts.value = false
  }
}
async function loadRecent() {
  loadingUsage.value = true
  try {
    const res = await usageAPI.getByDateRange(startDate.value, endDate.value)
    recentUsage.value = res.items.slice(0, 5)
  } catch (error) {
    console.error('Failed to load recent usage:', error)
  } finally {
    loadingUsage.value = false
  }
}
async function loadPlatformQuotas() {
  try {
    const data = await getMyPlatformQuotas()
    platformQuotas.value = data.platform_quotas ?? []
  } catch (error) {
    console.warn('Failed to load platform quotas:', error)
    platformQuotas.value = []
  }
}
function refreshAll() {
  void loadStats()
  void loadCharts()
  void loadRecent()
  void loadPlatformQuotas()
}

onMounted(refreshAll)
</script>

<style scoped>
.dashboard-page { background: #fff; }
.dashboard-heading { animation: dashboard-rise .55s ease-out both; }
.dashboard-refresh:hover:not(:disabled) { border-color: #b9baf5; color: #5658d7; box-shadow: 0 8px 18px rgba(100, 102, 233, .08); transform: translateY(-1px); }
.dashboard-refresh:active:not(:disabled) { transform: scale(.97); }
.dashboard-refresh:disabled { opacity: .55; cursor: not-allowed; }
.dashboard-section { border-top: 1px solid #e9eaf1; padding-top: 1.65rem; }
.dashboard-section-heading { display: flex; align-items: center; justify-content: space-between; margin-bottom: 1.25rem; }
.dashboard-section-heading h2 { color: #171825; font-size: 1.3rem; font-weight: 700; letter-spacing: -.02em; }
.dashboard-appear { animation: dashboard-rise .6s ease-out both; }
.dashboard-page :deep(.card) { border: 1px solid #e8e9f0; border-radius: .875rem; background: #fff; box-shadow: none; }
.dashboard-page :deep(.dashboard-metric-panel) { overflow: hidden; border: 1px solid #e8e9f0; border-radius: .875rem; }
.dashboard-page :deep(.dashboard-metric-panel .card) { border: 0; border-radius: 0; box-shadow: none; }
.dashboard-page :deep(.dashboard-metric-panel .card:hover) { background: #fafaff; }
.dashboard-page :deep(.dashboard-core-stats), .dashboard-page :deep(.dashboard-token-stats) { gap: 0; }
.dashboard-page :deep(.dashboard-token-stats) { border-top: 1px solid #e8e9f0; }
.dashboard-page :deep(.dashboard-core-stats > .card) { min-height: 8.6rem; padding: 1.25rem; }
.dashboard-page :deep(.dashboard-token-stats > .card) { min-height: 7.5rem; padding: 1.2rem; }
.dashboard-page :deep(.dashboard-balance-card) { background: #fafaff; }
.dashboard-page :deep(.dashboard-core-stats .text-xl) { font-size: 1.55rem; line-height: 1.3; }
.dashboard-page :deep(.dashboard-chart-grid > .card), .dashboard-page :deep(.dashboard-chart-grid > *) { min-height: 18rem; }
.dashboard-page :deep(.dashboard-chart-filters) { box-shadow: none; background: #fafaff; }
.dashboard-page :deep(.dashboard-platform-section) { padding: 1.4rem; }
.dashboard-page :deep(.card button:focus-visible) { outline: 2px solid #6466e9; outline-offset: 2px; }
@keyframes dashboard-rise { from { opacity: 0; transform: translateY(14px); } to { opacity: 1; transform: translateY(0); } }
@media (max-width: 640px) { .dashboard-page :deep(.dashboard-core-stats > .card) { min-height: 6.8rem; } .dashboard-page :deep(.dashboard-token-stats > .card) { min-height: 6.5rem; } }
@media (prefers-reduced-motion: reduce) { .dashboard-page *, .dashboard-page *::before, .dashboard-page *::after { animation: none !important; transition-duration: .01ms !important; } }
</style>

<style>
.dark .dashboard-page { background: #0b1120; }
.dark .dashboard-page .dashboard-section { border-color: #25314b; }
.dark .dashboard-page .dashboard-section-heading h2 { color: #f8fafc; }
.dark .dashboard-page .card { border-color: #2b344b; background: #151f33; box-shadow: none; }
.dark .dashboard-page .dashboard-metric-panel { border-color: #2b344b; }
.dark .dashboard-page .dashboard-metric-panel .card { border: 0; background: #151f33; }
.dark .dashboard-page .dashboard-metric-panel .card:hover { background: #1c2740; }
.dark .dashboard-page .dashboard-token-stats { border-color: #2b344b; }
.dark .dashboard-page .dashboard-balance-card { background: #1c2740 !important; }
.dark .dashboard-page .dashboard-chart-filters { background: #151f33; }
</style>
