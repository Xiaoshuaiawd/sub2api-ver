<template>
  <article class="group relative flex h-full flex-col overflow-hidden rounded-[28px] border border-slate-200/80 bg-white shadow-[0_12px_40px_-28px_rgba(15,23,42,0.35)] transition-[transform,box-shadow,border-color] duration-300 hover:-translate-y-1 hover:border-slate-300 hover:shadow-[0_28px_60px_-32px_rgba(15,23,42,0.4)] motion-reduce:transform-none motion-reduce:transition-none dark:border-dark-600 dark:bg-dark-800 dark:hover:border-dark-500">
    <div :class="['h-1.5 bg-gradient-to-r', tone.bar]" aria-hidden="true" />
    <div class="flex flex-1 flex-col p-5 sm:p-7">
      <div class="mb-7 flex items-start justify-between gap-3">
        <div :class="['flex h-12 w-12 shrink-0 items-center justify-center rounded-2xl bg-gradient-to-br text-white shadow-lg', tone.icon]" aria-hidden="true">
          <svg class="h-6 w-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
            <path d="m12 2 8 5v10l-8 5-8-5V7l8-5Z" />
            <path d="m4 7 8 5 8-5M12 12v10" />
          </svg>
        </div>
        <span :class="['inline-flex max-w-[60%] items-center rounded-full px-3 py-1 text-xs font-semibold', badgeLightClass]">{{ pLabel }}</span>
      </div>

      <h3 :title="plan.name" class="h-14 min-w-0 break-words text-xl font-bold leading-7 tracking-tight text-slate-950 [overflow-wrap:anywhere] line-clamp-2 dark:text-white">{{ plan.name }}</h3>
      <p v-if="plan.description" class="mt-2 min-h-10 text-sm leading-5 text-slate-500 line-clamp-2 dark:text-dark-300">{{ plan.description }}</p>

      <div class="mt-6 border-b border-slate-100 pb-6 dark:border-dark-600">
        <div class="flex flex-wrap items-baseline gap-x-1.5">
          <span class="text-2xl font-semibold text-slate-900 dark:text-white">{{ planCurrencySymbol }}</span>
          <span class="text-4xl font-bold tabular-nums tracking-tight text-slate-950 dark:text-white">{{ plan.price }}</span>
          <span v-if="plan.currency" class="text-xs font-medium text-slate-400 dark:text-dark-400">{{ plan.currency }}</span>
          <span class="ml-1 text-sm text-slate-500 dark:text-dark-300">/ {{ validitySuffix }}</span>
        </div>
        <div v-if="plan.original_price" class="mt-2 flex items-center gap-2">
          <span class="text-sm text-slate-400 line-through dark:text-dark-400">{{ planCurrencySymbol }}{{ plan.original_price }}<template v-if="plan.currency"> {{ plan.currency }}</template></span>
          <span v-if="discountText" :class="['rounded-md px-1.5 py-0.5 text-xs font-semibold', discountClass]">{{ discountText }}</span>
        </div>
      </div>

      <div class="mt-6 rounded-2xl bg-slate-50 p-4 dark:bg-dark-700/60">
        <p class="text-[11px] font-semibold uppercase tracking-[0.13em] text-slate-500 dark:text-dark-300">{{ t('payment.planCard.availableGroups') }}</p>
        <div v-if="plan.groups && plan.groups.length > 1" class="mt-2 flex flex-wrap gap-1.5">
          <span v-for="group in plan.groups" :key="group.id" class="rounded-lg border border-slate-200 bg-white px-2.5 py-1 text-xs font-medium text-slate-700 dark:border-dark-500 dark:bg-dark-800 dark:text-dark-200">{{ group.name }} (×{{ group.rate_multiplier }})</span>
        </div>
        <p v-else class="mt-1 text-sm font-semibold text-slate-800 dark:text-dark-100">{{ plan.group_name || plan.groups?.[0]?.name || pLabel }}</p>
        <p v-if="plan.groups && plan.groups.length > 1" class="mt-2 text-xs leading-5 text-slate-500 dark:text-dark-300">{{ t('payment.planCard.sharedQuotaHint') }}</p>
      </div>

      <div class="mt-5 grid grid-cols-2 gap-3">
        <div class="rounded-xl border border-slate-100 px-3 py-2.5 dark:border-dark-600">
          <p class="text-xs text-slate-500 dark:text-dark-300">{{ t('payment.planCard.rate') }}</p>
          <p class="mt-0.5 text-base font-semibold text-slate-900 dark:text-white">{{ rateDisplay }}</p>
        </div>
        <div v-if="plan.daily_limit_usd != null" class="rounded-xl border border-slate-100 px-3 py-2.5 dark:border-dark-600">
          <p class="text-xs text-slate-500 dark:text-dark-300">{{ t('payment.planCard.dailyLimit') }}</p>
          <p class="mt-0.5 text-base font-semibold text-slate-900 dark:text-white">${{ plan.daily_limit_usd }}</p>
        </div>
        <div v-if="plan.weekly_limit_usd != null" class="rounded-xl border border-slate-100 px-3 py-2.5 dark:border-dark-600">
          <p class="text-xs text-slate-500 dark:text-dark-300">{{ t('payment.planCard.weeklyLimit') }}</p>
          <p class="mt-0.5 text-base font-semibold text-slate-900 dark:text-white">${{ plan.weekly_limit_usd }}</p>
        </div>
        <div v-if="plan.monthly_limit_usd != null" class="rounded-xl border border-slate-100 px-3 py-2.5 dark:border-dark-600">
          <p class="text-xs text-slate-500 dark:text-dark-300">{{ t('payment.planCard.monthlyLimit') }}</p>
          <p class="mt-0.5 text-base font-semibold text-slate-900 dark:text-white">${{ plan.monthly_limit_usd }}</p>
        </div>
        <div v-if="plan.daily_limit_usd == null && plan.weekly_limit_usd == null && plan.monthly_limit_usd == null" class="rounded-xl border border-slate-100 px-3 py-2.5 dark:border-dark-600">
          <p class="text-xs text-slate-500 dark:text-dark-300">{{ t('payment.planCard.quota') }}</p>
          <p class="mt-0.5 text-base font-semibold text-slate-900 dark:text-white">{{ t('payment.planCard.unlimited') }}</p>
        </div>
      </div>
      <p v-if="hasPeakRate" class="mt-3 text-xs leading-5 text-amber-700 dark:text-amber-300">{{ t('payment.planCard.peakRate') }} · {{ peakRateDisplay }}</p>
      <div v-if="modelScopeLabels.length > 0" class="mt-4 flex flex-wrap items-center gap-1.5 text-xs">
        <span class="mr-1 text-slate-500 dark:text-dark-300">{{ t('payment.planCard.models') }}</span>
        <span v-for="scope in modelScopeLabels" :key="scope" class="rounded-full bg-slate-100 px-2 py-1 font-medium text-slate-700 dark:bg-dark-700 dark:text-dark-200">{{ scope }}</span>
      </div>

      <ul v-if="plan.features.length > 0" class="mt-6 space-y-2.5 border-t border-slate-100 pt-5 dark:border-dark-600">
        <li v-for="feature in plan.features" :key="feature" class="flex items-start gap-2.5 text-sm leading-5 text-slate-600 dark:text-dark-200">
          <svg :class="['mt-0.5 h-4 w-4 shrink-0', tone.check]" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5" /></svg>
          <span>{{ feature }}</span>
        </li>
      </ul>

      <div class="flex-1 min-h-6" />
      <button type="button" :class="['flex min-h-12 w-full items-center justify-center gap-2 rounded-xl px-4 py-3 text-sm font-semibold text-white shadow-sm transition-[transform,filter,box-shadow] duration-200 hover:brightness-110 hover:shadow-lg active:scale-[0.98] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 motion-reduce:transform-none motion-reduce:transition-none', tone.button]" @click="emit('select', plan)">
        {{ isRenewal ? t('payment.renewNow') : t('payment.subscribeNow') }}
        <svg class="h-4 w-4 transition-transform duration-200 group-hover:translate-x-1 motion-reduce:transform-none" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M5 12h14m-6-6 6 6-6 6" /></svg>
      </button>
    </div>
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
import {
  platformBadgeLightClass,
  platformDiscountClass,
  platformLabel,
} from '@/utils/platformColors'

const props = withDefaults(defineProps<{ plan: SubscriptionPlan; activeSubscriptions?: UserSubscription[]; tierIndex?: number }>(), { tierIndex: 0 })
const emit = defineEmits<{ select: [plan: SubscriptionPlan] }>()
const { t } = useI18n()

const platform = computed(() => props.plan.group_platform || '')
const isRenewal = computed(() =>
  props.activeSubscriptions?.some(s => s.group_id === props.plan.group_id && s.status === 'active') ?? false
)

const badgeLightClass = computed(() => platformBadgeLightClass(platform.value))
const discountClass = computed(() => platformDiscountClass(platform.value))
const tierTones = [
  { bar: 'from-teal-400 to-emerald-500', icon: 'from-teal-400 to-emerald-500 shadow-teal-500/20', button: 'bg-teal-600 focus-visible:outline-teal-600', check: 'text-teal-500' },
  { bar: 'from-sky-400 to-blue-600', icon: 'from-sky-400 to-blue-600 shadow-blue-500/20', button: 'bg-blue-600 focus-visible:outline-blue-600', check: 'text-blue-500' },
  { bar: 'from-violet-400 to-indigo-600', icon: 'from-violet-400 to-indigo-600 shadow-indigo-500/20', button: 'bg-indigo-600 focus-visible:outline-indigo-600', check: 'text-indigo-500' },
  { bar: 'from-amber-400 to-orange-500', icon: 'from-amber-400 to-orange-500 shadow-orange-500/20', button: 'bg-orange-600 focus-visible:outline-orange-600', check: 'text-orange-500' },
] as const
const tone = computed(() => tierTones[props.tierIndex % tierTones.length]!)
const pLabel = computed(() => new Set(props.plan.groups?.map(group => group.platform) || []).size > 1
  ? t('payment.planCard.multiplePlatforms')
  : platformLabel(platform.value))

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

const appStore = useAppStore()
const planCurrencySymbol = computed(() => currencySymbol(props.plan.currency || 'USD'))

const hasPeakRate = computed(() => (props.plan.groups?.length || 0) <= 1 && groupHasPeakRate(props.plan))

const peakRateDisplay = computed(() => {
  return formatPeakRateWindow(props.plan, serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))
})

const MODEL_SCOPE_LABELS: Record<string, string> = {
  claude: 'Claude',
  gemini_text: 'Gemini',
  gemini_image: 'Imagen',
}

const modelScopeLabels = computed(() => {
  if ((props.plan.groups?.length || 0) > 1) return []
  if (platform.value !== 'antigravity') return []
  const scopes = props.plan.supported_model_scopes
  if (!scopes || scopes.length === 0) return []
  return scopes.map(s => MODEL_SCOPE_LABELS[s] || s)
})

const validitySuffix = computed(() => planValiditySuffix(props.plan, t))
</script>
