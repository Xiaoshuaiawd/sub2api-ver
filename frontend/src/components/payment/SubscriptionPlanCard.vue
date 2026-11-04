<template>
  <article class="group flex h-full flex-col rounded-[24px] border border-slate-200 bg-white p-6 shadow-[0_8px_28px_-24px_rgba(15,23,42,0.25)] transition-[border-color,box-shadow,transform] duration-200 hover:-translate-y-1 hover:border-slate-300 hover:shadow-[0_22px_45px_-30px_rgba(15,23,42,0.3)] motion-reduce:transform-none motion-reduce:transition-none dark:border-dark-600 dark:bg-dark-800 dark:hover:border-dark-500 sm:p-7">
    <div class="flex items-center justify-between gap-3">
      <span class="text-[11px] font-semibold uppercase tracking-[0.16em] text-slate-400 dark:text-dark-400">{{ t('payment.subscriptionHeroEyebrow') }}</span>
      <span class="shrink-0 text-xs font-medium text-slate-500 dark:text-dark-300">{{ pLabel }}</span>
    </div>

    <h3 :title="plan.name" class="mt-6 h-14 min-w-0 break-words text-xl font-bold leading-7 tracking-tight text-slate-950 [overflow-wrap:anywhere] line-clamp-2 dark:text-white">{{ plan.name }}</h3>
    <p v-if="displayDescription" class="mt-1.5 text-sm leading-6 text-slate-500 line-clamp-2 dark:text-dark-300">{{ displayDescription }}</p>

    <div class="mt-5 flex flex-wrap items-baseline gap-x-1.5">
      <span class="text-xl font-semibold text-slate-900 dark:text-white">{{ planCurrencySymbol }}</span>
      <span class="text-[2.5rem] font-bold leading-none tabular-nums tracking-tight text-slate-950 dark:text-white">{{ plan.price }}</span>
      <span v-if="plan.currency" class="text-xs font-medium text-slate-400 dark:text-dark-400">{{ plan.currency }}</span>
      <span class="ml-1 text-sm text-slate-500 dark:text-dark-300">/ {{ validitySuffix }}</span>
    </div>
    <div v-if="plan.original_price" class="mt-2 flex items-center gap-2">
      <span class="text-sm text-slate-400 line-through dark:text-dark-400">{{ planCurrencySymbol }}{{ plan.original_price }}<template v-if="plan.currency"> {{ plan.currency }}</template></span>
      <span v-if="discountText" :class="['rounded-md px-1.5 py-0.5 text-xs font-semibold', discountClass]">{{ discountText }}</span>
    </div>

    <div data-testid="primary-quota" class="mt-7 rounded-2xl bg-slate-50 px-4 py-4 dark:bg-dark-700/60">
      <p class="text-xs font-medium text-slate-500 dark:text-dark-300">{{ primaryLimit.label }}</p>
      <p class="mt-1 text-2xl font-bold tabular-nums tracking-tight text-slate-900 dark:text-white">{{ primaryLimit.value }}</p>
    </div>

    <div class="mt-5 space-y-3 text-sm">
      <div class="flex items-start justify-between gap-3">
        <span class="shrink-0 text-slate-500 dark:text-dark-300">{{ t('payment.planCard.availableGroups') }}</span>
        <div v-if="plan.groups && plan.groups.length > 1" class="flex flex-wrap justify-end gap-1.5 text-right">
          <span v-for="group in plan.groups" :key="group.id" class="rounded-md bg-slate-100 px-2 py-0.5 text-xs font-medium text-slate-700 dark:bg-dark-700 dark:text-dark-200">{{ group.name }} (×{{ group.rate_multiplier }})</span>
        </div>
        <span v-else class="text-right font-medium text-slate-800 dark:text-dark-100">{{ plan.group_name || plan.groups?.[0]?.name || pLabel }}</span>
      </div>
      <p v-if="plan.groups && plan.groups.length > 1" class="text-xs leading-5 text-slate-500 dark:text-dark-300">{{ t('payment.planCard.sharedQuotaHint') }}</p>
      <div v-for="limit in secondaryLimits" :key="limit.label" class="flex items-center justify-between gap-3">
        <span class="text-slate-500 dark:text-dark-300">{{ limit.label }}</span>
        <span class="font-semibold tabular-nums text-slate-800 dark:text-dark-100">{{ limit.value }}</span>
      </div>
      <div class="flex items-center justify-between gap-3">
        <span class="text-slate-500 dark:text-dark-300">{{ t('payment.planCard.rate') }}</span>
        <span class="font-semibold text-slate-800 dark:text-dark-100">{{ rateDisplay }}</span>
      </div>
      <p v-if="hasPeakRate" class="text-xs leading-5 text-amber-700 dark:text-amber-300">{{ t('payment.planCard.peakRate') }} · {{ peakRateDisplay }}</p>
      <div v-if="modelScopeLabels.length > 0" class="flex flex-wrap items-center gap-1.5 text-xs">
        <span class="mr-1 text-slate-500 dark:text-dark-300">{{ t('payment.planCard.models') }}</span>
        <span v-for="scope in modelScopeLabels" :key="scope" class="rounded-full bg-slate-100 px-2 py-1 font-medium text-slate-700 dark:bg-dark-700 dark:text-dark-200">{{ scope }}</span>
      </div>
    </div>

    <ul v-if="plan.features.length > 0" class="mt-6 space-y-2.5 border-t border-slate-100 pt-5 dark:border-dark-600">
      <li v-for="feature in plan.features" :key="feature" class="flex items-start gap-2.5 text-sm leading-5 text-slate-600 dark:text-dark-200">
        <svg class="mt-0.5 h-4 w-4 shrink-0 text-teal-600 dark:text-teal-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5" /></svg>
        <span>{{ feature }}</span>
      </li>
    </ul>

    <div class="min-h-6 flex-1" />
    <button type="button" class="flex min-h-12 w-full items-center justify-center gap-2 rounded-xl bg-slate-950 px-4 py-3 text-sm font-semibold text-white transition-[background-color,transform] duration-200 hover:bg-slate-800 active:scale-[0.98] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-slate-900 motion-reduce:transform-none motion-reduce:transition-none dark:bg-white dark:text-slate-950 dark:hover:bg-slate-200" @click="emit('select', plan)">
      {{ isRenewal ? t('payment.renewNow') : t('payment.subscribeNow') }}
      <svg class="h-4 w-4 transition-transform duration-200 group-hover:translate-x-1 motion-reduce:transform-none" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M5 12h14m-6-6 6 6-6 6" /></svg>
    </button>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SubscriptionPlan } from '@/types/payment'
import type { UserSubscription } from '@/types'
import { useAppStore } from '@/stores/app'
import { hasPeakRate as groupHasPeakRate, formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import { planValiditySuffix } from './validity'
import { currencySymbol } from '@/components/payment/currency'
import { platformDiscountClass, platformLabel } from '@/utils/platformColors'

const props = defineProps<{ plan: SubscriptionPlan; activeSubscriptions?: UserSubscription[] }>()
const emit = defineEmits<{ select: [plan: SubscriptionPlan] }>()
const { t } = useI18n()

const platform = computed(() => props.plan.group_platform || '')
const isRenewal = computed(() => props.activeSubscriptions?.some(s => s.group_id === props.plan.group_id && s.status === 'active') ?? false)
const discountClass = computed(() => platformDiscountClass(platform.value))
const pLabel = computed(() => new Set(props.plan.groups?.map(group => group.platform) || []).size > 1
  ? t('payment.planCard.multiplePlatforms')
  : platformLabel(platform.value))

// Some older plans store the same group and quota fields in their description.
const displayDescription = computed(() => {
  const description = props.plan.description?.trim() || ''
  if (/^(可用分组|Available groups)\s*[:：]/i.test(description) && /(日限额|周限额|月限额|daily|weekly|monthly)/i.test(description)) return ''
  return description
})

const discountText = computed(() => {
  if (!props.plan.original_price || props.plan.original_price <= 0) return ''
  const pct = Math.round((1 - props.plan.price / props.plan.original_price) * 100)
  return pct > 0 ? `-${pct}%` : ''
})

const rateDisplay = computed(() => {
  if ((props.plan.groups?.length || 0) > 1) return t('payment.planCard.variesByGroup')
  const rate = props.plan.rate_multiplier ?? 1
  return `×${Number(rate.toPrecision(10))}`
})

const primaryLimit = computed(() => {
  if (props.plan.monthly_limit_usd != null) return { label: t('payment.planCard.monthlyLimit'), value: `$${props.plan.monthly_limit_usd}` }
  if (props.plan.weekly_limit_usd != null) return { label: t('payment.planCard.weeklyLimit'), value: `$${props.plan.weekly_limit_usd}` }
  if (props.plan.daily_limit_usd != null) return { label: t('payment.planCard.dailyLimit'), value: `$${props.plan.daily_limit_usd}` }
  return { label: t('payment.planCard.quota'), value: t('payment.planCard.unlimited') }
})

const secondaryLimits = computed(() => {
  const limits = []
  if (props.plan.daily_limit_usd != null && primaryLimit.value.label !== t('payment.planCard.dailyLimit')) limits.push({ label: t('payment.planCard.dailyLimit'), value: `$${props.plan.daily_limit_usd}` })
  if (props.plan.weekly_limit_usd != null && primaryLimit.value.label !== t('payment.planCard.weeklyLimit')) limits.push({ label: t('payment.planCard.weeklyLimit'), value: `$${props.plan.weekly_limit_usd}` })
  return limits
})

const appStore = useAppStore()
const planCurrencySymbol = computed(() => currencySymbol(props.plan.currency || 'USD'))
const hasPeakRate = computed(() => (props.plan.groups?.length || 0) <= 1 && groupHasPeakRate(props.plan))
const peakRateDisplay = computed(() => formatPeakRateWindow(props.plan, serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset)))

const MODEL_SCOPE_LABELS: Record<string, string> = { claude: 'Claude', gemini_text: 'Gemini', gemini_image: 'Imagen' }
const modelScopeLabels = computed(() => {
  if ((props.plan.groups?.length || 0) > 1 || platform.value !== 'antigravity') return []
  const scopes = props.plan.supported_model_scopes
  return scopes?.map(s => MODEL_SCOPE_LABELS[s] || s) || []
})
const validitySuffix = computed(() => planValiditySuffix(props.plan, t))
</script>
