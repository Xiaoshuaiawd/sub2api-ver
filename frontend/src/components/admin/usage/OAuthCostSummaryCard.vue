<template>
  <section class="card p-4" aria-labelledby="oauth-cost-title">
    <div class="flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between">
      <div class="min-w-0">
        <div class="flex items-center gap-3">
          <div class="rounded-lg bg-emerald-100 p-2 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-400">
            <Icon name="calculator" size="md" />
          </div>
          <div>
            <h2 id="oauth-cost-title" class="text-base font-semibold text-gray-900 dark:text-white">
              {{ t('usage.oauthCost.title') }}
            </h2>
            <p class="mt-0.5 text-sm text-gray-500 dark:text-dark-300">
              {{ t('usage.oauthCost.description') }}
            </p>
          </div>
        </div>
      </div>

      <form class="flex w-full flex-col gap-2 sm:w-auto sm:min-w-[22rem]" @submit.prevent="savePurchaseCost">
        <label for="oauth-purchase-cost" class="input-label mb-0">
          {{ t('usage.oauthCost.purchaseCost') }}
        </label>
        <div class="flex gap-2">
          <div class="relative min-w-0 flex-1">
            <span class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-sm text-gray-400">
              ¥
            </span>
            <input
              id="oauth-purchase-cost"
              v-model="purchaseCostInput"
              type="number"
              min="0"
              step="0.01"
              inputmode="decimal"
              class="input pl-8"
              :class="{ 'input-error': purchaseCostError }"
              :disabled="loading || saving"
              :aria-invalid="purchaseCostError ? 'true' : 'false'"
            />
          </div>
          <button type="submit" class="btn btn-primary shrink-0" :disabled="loading || saving">
            <Icon v-if="!saving" name="check" size="sm" />
            <span>{{ saving ? t('usage.oauthCost.saving') : t('usage.oauthCost.save') }}</span>
          </button>
        </div>
        <p v-if="purchaseCostError" class="input-error-text" role="alert">
          {{ t('usage.oauthCost.invalidPurchaseCost') }}
        </p>
        <p v-else-if="saveState === 'saved'" class="text-xs text-emerald-600 dark:text-emerald-400" aria-live="polite">
          {{ t('usage.oauthCost.saved') }}
        </p>
        <p v-else-if="saveState === 'failed'" class="input-error-text" role="alert">
          {{ t('usage.oauthCost.saveFailed') }}
        </p>
      </form>
    </div>

    <div v-if="loading" class="mt-5 grid grid-cols-1 gap-3 md:grid-cols-4">
      <div v-for="index in 4" :key="index" class="h-20 animate-pulse rounded-xl bg-gray-100 dark:bg-dark-700/60" />
    </div>

    <div
      v-else-if="loadFailed"
      class="mt-5 flex flex-col gap-3 rounded-xl border border-red-100 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300 sm:flex-row sm:items-center sm:justify-between"
      role="alert"
    >
      <span>{{ t('usage.oauthCost.loadFailed') }}</span>
      <button type="button" class="btn btn-secondary btn-sm self-start sm:self-auto" @click="loadData">
        <Icon name="refresh" size="sm" />
        <span>{{ t('usage.oauthCost.retry') }}</span>
      </button>
    </div>

    <div v-else class="mt-5 grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4">
      <div class="rounded-xl border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900/40">
        <p class="text-xs font-medium text-gray-500 dark:text-dark-300">{{ t('usage.oauthCost.consumedQuota') }}</p>
        <p data-testid="oauth-total-consumed-quota" class="mt-2 text-xl font-semibold tabular-nums text-gray-900 dark:text-white">
          {{ formatQuota(summary?.total_consumed_quota ?? 0) }}
        </p>
      </div>

      <div class="rounded-xl border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900/40">
        <p class="text-xs font-medium text-gray-500 dark:text-dark-300">{{ t('usage.oauthCost.unitCost') }}</p>
        <p data-testid="oauth-unit-cost" class="mt-2 text-xl font-semibold tabular-nums text-emerald-600 dark:text-emerald-400">
          <span v-if="hasConsumedQuota">¥{{ formatQuota(unitCost) }}</span>
          <span v-else class="text-sm font-medium text-gray-500 dark:text-dark-300">
            {{ t('usage.oauthCost.unitCostUnavailable') }}
          </span>
        </p>
      </div>

      <div class="rounded-xl border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900/40">
        <p class="text-xs font-medium text-gray-500 dark:text-dark-300">{{ t('usage.oauthCost.accountCount') }}</p>
        <p class="mt-2 text-xl font-semibold tabular-nums text-gray-900 dark:text-white">
          {{ formatInteger(summary?.account_count ?? 0) }}
        </p>
      </div>

      <div class="rounded-xl border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900/40">
        <p class="text-xs font-medium text-gray-500 dark:text-dark-300">
          {{ t('usage.oauthCost.requests') }} / {{ t('usage.oauthCost.tokens') }}
        </p>
        <p class="mt-2 text-xl font-semibold tabular-nums text-gray-900 dark:text-white">
          {{ formatInteger(summary?.total_requests ?? 0) }}
        </p>
        <p class="mt-1 text-xs text-gray-500 dark:text-dark-300">
          {{ formatInteger(summary?.total_tokens ?? 0) }} {{ t('usage.oauthCost.tokens') }}
        </p>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getOAuthCostSummary, type OAuthCostSummaryResponse } from '@/api/admin/usage'
import { getOAuthCost, updateOAuthCost } from '@/api/admin/settings'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()

const summary = ref<OAuthCostSummaryResponse | null>(null)
const purchaseCostInput = ref('0')
const loading = ref(true)
const saving = ref(false)
const loadFailed = ref(false)
const saveState = ref<'idle' | 'saved' | 'failed'>('idle')
const purchaseCostError = ref(false)

const numericPurchaseCost = computed(() => Number(purchaseCostInput.value))
const hasConsumedQuota = computed(() => (summary.value?.total_consumed_quota ?? 0) > 0)
const unitCost = computed(() => {
  const consumedQuota = summary.value?.total_consumed_quota ?? 0
  if (consumedQuota <= 0) return 0
  return numericPurchaseCost.value / consumedQuota
})

const formatQuota = (value: number) => {
  if (!Number.isFinite(value)) return '0.000000'
  return value.toFixed(6)
}

const formatInteger = (value: number) => {
  if (!Number.isFinite(value)) return '0'
  return Math.trunc(value).toLocaleString()
}

const loadData = async () => {
  loading.value = true
  loadFailed.value = false
  saveState.value = 'idle'
  try {
    const [summaryResult, costResult] = await Promise.all([
      getOAuthCostSummary(),
      getOAuthCost(),
    ])
    summary.value = summaryResult
    purchaseCostInput.value = String(costResult.purchase_cost_cny ?? 0)
  } catch {
    loadFailed.value = true
  } finally {
    loading.value = false
  }
}

const savePurchaseCost = async () => {
  const amount = numericPurchaseCost.value
  purchaseCostError.value = !Number.isFinite(amount) || amount < 0
  saveState.value = 'idle'
  if (purchaseCostError.value) return

  saving.value = true
  try {
    const saved = await updateOAuthCost({ purchase_cost_cny: amount })
    purchaseCostInput.value = String(saved.purchase_cost_cny)
    saveState.value = 'saved'
  } catch {
    saveState.value = 'failed'
  } finally {
    saving.value = false
  }
}

onMounted(loadData)
</script>
