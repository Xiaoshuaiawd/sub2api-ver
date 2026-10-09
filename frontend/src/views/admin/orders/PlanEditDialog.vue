<template>
  <BaseDialog :show="show" :title="plan ? t('payment.admin.editPlan') : t('payment.admin.createPlan')" width="wide" @close="emit('close')">
    <form id="plan-form" @submit.prevent="handleSavePlan" class="space-y-4">
      <div>
        <label class="input-label">{{ t('payment.admin.planName') }} <span class="text-red-500">*</span></label>
        <input v-model="planForm.name" type="text" class="input" required />
      </div>
      <div>
        <label class="input-label" for="plan-group-search">{{ t('payment.admin.groups') }} <span class="text-red-500">*</span></label>
        <input id="plan-group-search" v-model="groupSearch" type="search" class="input" :placeholder="t('payment.admin.searchGroups')" />
        <div role="group" :aria-label="t('payment.admin.groups')" class="mt-2 max-h-44 space-y-1 overflow-y-auto rounded-lg border border-gray-200 p-2 dark:border-dark-600">
          <label v-for="group in filteredGroupOptions" :key="group.id" class="flex min-h-10 cursor-pointer items-center gap-2 rounded px-2 py-1.5 hover:bg-gray-50 dark:hover:bg-dark-700">
            <input type="checkbox" :checked="planForm.group_ids.includes(group.id)" :value="group.id" @change="toggleGroup(group.id)" />
            <span :class="platformTextClass(group.platform)">{{ group.name }} · {{ group.platform }} ({{ subscriptionPlanRate(group) }}x)<span v-if="group.status !== 'active'" class="ml-1 text-red-600 dark:text-red-400">({{ group.status }})</span></span>
          </label>
          <p v-if="filteredGroupOptions.length === 0" class="px-2 py-1 text-sm text-gray-500">{{ t('payment.admin.noMatchingGroups') }}</p>
        </div>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.groupsSelected', { count: planForm.group_ids.length }) }}</p>
      </div>
      <div v-if="selectedGroupInfos.length" class="flex flex-wrap gap-2">
        <GroupBadge v-for="group in selectedGroupInfos" :key="group.id" :name="group.name" :platform="group.platform" :rate-multiplier="subscriptionPlanRate(group)" />
      </div>
      <fieldset class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
        <legend class="px-1 text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('payment.admin.sharedQuota') }}</legend>
        <p class="mb-3 text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.sharedQuotaHint') }}</p>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <div><label class="input-label" for="plan-daily-limit">{{ t('payment.admin.dailyLimit') }}</label><input id="plan-daily-limit" v-model.number="planForm.daily_limit_usd" type="number" min="0" step="0.00000001" class="input" /></div>
          <div><label class="input-label" for="plan-weekly-limit">{{ t('payment.admin.weeklyLimit') }}</label><input id="plan-weekly-limit" v-model.number="planForm.weekly_limit_usd" type="number" min="0" step="0.00000001" class="input" /></div>
          <div><label class="input-label" for="plan-monthly-limit">{{ t('payment.admin.monthlyLimit') }}</label><input id="plan-monthly-limit" v-model.number="planForm.monthly_limit_usd" type="number" min="0" step="0.00000001" class="input" /></div>
        </div>
      </fieldset>

      <div><label class="input-label">{{ t('payment.admin.planDescription') }} <span class="text-red-500">*</span></label><textarea v-model="planForm.description" rows="2" class="input" required></textarea></div>
      <div class="grid grid-cols-2 gap-4">
        <div>
          <label class="input-label">{{ t('payment.admin.price') }} <span class="text-red-500">*</span></label>
          <input id="plan-price" v-model.number="planForm.price" type="number" step="0.01" min="0.01" class="input" required />
          <p v-if="subscriptionCnyPreview" class="mt-1 text-xs font-medium text-primary-600 dark:text-primary-400">
            {{ t('payment.admin.subscriptionCnyPayPreview', { amount: subscriptionCnyPreview.amount }) }}
            <span v-if="subscriptionCnyPreview.feeRate > 0">
              {{ t('payment.admin.subscriptionCnyPayPreviewWithFee', { feeRate: subscriptionCnyPreview.feeRate, total: subscriptionCnyPreview.total }) }}
            </span>
          </p>
        </div>
        <div><label class="input-label">{{ t('payment.admin.originalPrice') }}</label><input v-model.number="planForm.original_price" type="number" step="0.01" min="0" class="input" /></div>
      </div>
      <div class="grid grid-cols-2 gap-4">
        <div><label class="input-label">{{ t('payment.admin.validity') }} <span class="text-red-500">*</span></label><input v-model.number="planForm.validity_days" type="number" min="1" class="input" required /></div>
        <div><label class="input-label">{{ t('payment.admin.validityUnit') }} <span class="text-red-500">*</span></label><Select v-model="planForm.validity_unit" :options="validityUnitOptions" /></div>
      </div>
      <div class="grid grid-cols-2 gap-4">
        <div><label class="input-label">{{ t('payment.admin.sortOrder') }}</label><input v-model.number="planForm.sort_order" type="number" min="0" class="input" /></div>
        <div>
          <label class="input-label">{{ t('payment.admin.currency') }}</label>
          <input v-model="planForm.currency" type="text" maxlength="3" class="input uppercase" :placeholder="t('payment.admin.currencyPlaceholder')" />
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.currencyHint') }}</p>
        </div>
      </div>
      <div>
        <label class="input-label">{{ t('payment.admin.features') }}</label>
        <textarea v-model="planFeaturesText" rows="3" class="input" :placeholder="t('payment.admin.featuresPlaceholder')"></textarea>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.featuresHint') }}</p>
      </div>
      <div class="flex items-center gap-3">
        <label class="text-sm text-gray-700 dark:text-gray-300">{{ t('payment.admin.forSale') }}</label>
        <button
          type="button"
          :class="[
            'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
            planForm.for_sale ? 'bg-primary-500' : 'bg-gray-300 dark:bg-dark-600'
          ]"
          @click="planForm.for_sale = !planForm.for_sale"
        >
          <span :class="[
            'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
            planForm.for_sale ? 'translate-x-5' : 'translate-x-0'
          ]" />
        </button>
      </div>
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" @click="emit('close')" class="btn btn-secondary">{{ t('common.cancel') }}</button>
        <button type="submit" form="plan-form" :disabled="saving" class="btn btn-primary">{{ saving ? t('common.saving') : t('common.save') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminPaymentAPI } from '@/api/admin/payment'
import type { AdminPaymentConfig } from '@/api/admin/payment'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatPaymentAmount } from '@/components/payment/currency'
import type { SubscriptionPlan } from '@/types/payment'
import type { AdminGroup } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import { platformTextClass } from '@/utils/platformColors'

const props = defineProps<{
  show: boolean
  plan: SubscriptionPlan | null
  groups: AdminGroup[]
  paymentConfig?: AdminPaymentConfig | null
}>()

const emit = defineEmits<{
  close: []
  saved: []
}>()

const { t } = useI18n()
const appStore = useAppStore()

const saving = ref(false)
const planForm = reactive({ name: '', group_ids: [] as number[], daily_limit_usd: 0, weekly_limit_usd: 0, monthly_limit_usd: 0, description: '', price: 0, original_price: 0, currency: '', validity_days: 30, validity_unit: 'days', sort_order: 0, for_sale: true })
const planFeaturesText = ref('')
const groupSearch = ref('')

const validityUnitOptions = computed(() => [
  { value: 'days', label: t('payment.admin.days') },
  { value: 'weeks', label: t('payment.admin.weeks') },
  { value: 'months', label: t('payment.admin.months') },
])

const groupOptions = computed(() =>
  props.groups
    .filter(g => (g.subscription_type === 'subscription' || g.subscription_type === 'subscription_balance') && (g.status === 'active' || planForm.group_ids.includes(g.id))),
)

const filteredGroupOptions = computed(() => groupOptions.value.filter(g =>
  `${g.name} ${g.platform}`.toLowerCase().includes(groupSearch.value.trim().toLowerCase()),
))
const selectedGroupInfos = computed(() => planForm.group_ids
  .map(id => props.groups.find(g => g.id === id))
  .filter((g): g is AdminGroup => Boolean(g)))

function subscriptionPlanRate(group: AdminGroup): number {
  return group.subscription_type === 'subscription_balance'
    ? group.subscription_rate_multiplier ?? group.rate_multiplier
    : group.rate_multiplier
}

function toggleGroup(id: number) {
  const index = planForm.group_ids.indexOf(id)
  if (index >= 0) planForm.group_ids.splice(index, 1)
  else planForm.group_ids.push(id)
}

function roundCnyAmount(value: number): number {
  return Math.round(value * 100) / 100
}

function ceilCnyAmount(value: number): number {
  return Math.ceil(value * 100) / 100
}

const subscriptionCnyPreview = computed(() => {
  const price = Number(planForm.price) || 0
  const rate = Number(props.paymentConfig?.subscription_usd_to_cny_rate) || 0
  if (price <= 0 || rate <= 0) return null

  const amount = roundCnyAmount(price * rate)
  const feeRate = Number(props.paymentConfig?.recharge_fee_rate) || 0
  const fee = feeRate > 0 ? ceilCnyAmount((amount * feeRate) / 100) : 0
  const total = feeRate > 0 ? roundCnyAmount(amount + fee) : amount

  return {
    amount: formatPaymentAmount(amount, 'CNY'),
    feeRate,
    total: formatPaymentAmount(total, 'CNY'),
  }
})

// Reset form when dialog opens
watch(() => props.show, (visible) => {
  if (!visible) return
  groupSearch.value = ''
  if (props.plan) {
    Object.assign(planForm, { name: props.plan.name, group_ids: [...(props.plan.group_ids?.length ? props.plan.group_ids : [props.plan.group_id])], daily_limit_usd: props.plan.daily_limit_usd ?? 0, weekly_limit_usd: props.plan.weekly_limit_usd ?? 0, monthly_limit_usd: props.plan.monthly_limit_usd ?? 0, description: props.plan.description, price: props.plan.price, original_price: props.plan.original_price || 0, currency: props.plan.currency || '', validity_days: props.plan.validity_days, validity_unit: props.plan.validity_unit || 'days', sort_order: props.plan.sort_order || 0, for_sale: props.plan.for_sale })
    planFeaturesText.value = (props.plan.features || []).join('\n')
  } else {
    Object.assign(planForm, { name: '', group_ids: [], daily_limit_usd: 0, weekly_limit_usd: 0, monthly_limit_usd: 0, description: '', price: 0, original_price: 0, currency: '', validity_days: 30, validity_unit: 'days', sort_order: 0, for_sale: true })
    planFeaturesText.value = ''
  }
})

/** Build request payload with snake_case keys matching backend JSON tags */
function buildPlanPayload() {
  const features = planFeaturesText.value.split('\n').map(f => f.trim()).filter(Boolean).join('\n')
  return {
    name: planForm.name,
    group_id: planForm.group_ids[0],
    group_ids: [...planForm.group_ids],
    daily_limit_usd: Number(planForm.daily_limit_usd) || 0,
    weekly_limit_usd: Number(planForm.weekly_limit_usd) || 0,
    monthly_limit_usd: Number(planForm.monthly_limit_usd) || 0,
    description: planForm.description,
    price: planForm.price,
    original_price: planForm.original_price || 0,
    currency: planForm.currency.trim().toUpperCase(),
    validity_days: planForm.validity_days,
    validity_unit: planForm.validity_unit,
    sort_order: planForm.sort_order,
    for_sale: planForm.for_sale,
    features,
  }
}

async function handleSavePlan() {
  if (planForm.group_ids.length === 0) {
    appStore.showError(t('payment.admin.groupRequired'))
    return
  }
  if (!planForm.price || planForm.price <= 0) {
    appStore.showError(t('payment.admin.priceRequired'))
    return
  }
  if (!planForm.validity_days || planForm.validity_days < 1) {
    appStore.showError(t('payment.admin.validityRequired'))
    return
  }
  saving.value = true
  try {
    const data = buildPlanPayload()
    if (props.plan) { await adminPaymentAPI.updatePlan(props.plan.id, data) }
    else { await adminPaymentAPI.createPlan(data) }
    appStore.showSuccess(t('common.saved'))
    emit('close')
    emit('saved')
  } catch (err: unknown) { appStore.showError(extractApiErrorMessage(err, t('common.error'))) }
  finally { saving.value = false }
}
</script>
