<template>
  <BaseDialog :show="show" :title="t('admin.usage.message.title')" width="wide" @close="close">
    <div v-if="loading" class="flex min-h-72 items-center justify-center">
      <LoadingSpinner />
    </div>
    <div v-else-if="loadError" class="flex min-h-72 flex-col items-center justify-center gap-3 text-center">
      <Icon name="exclamationCircle" size="lg" class="text-rose-500" />
      <p class="text-sm text-gray-600 dark:text-gray-300">{{ loadError }}</p>
      <button class="btn btn-secondary" type="button" @click="load">{{ t('admin.usage.message.retry') }}</button>
    </div>
    <div v-else-if="detail" class="space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 pb-3 dark:border-dark-700">
        <div class="min-w-0 text-xs text-gray-500 dark:text-gray-400">
          <span class="font-medium text-gray-700 dark:text-gray-200">#{{ detail.usage_log_id }}</span>
          <span class="mx-2">·</span>
          <span class="break-all">{{ detail.request_id }}</span>
        </div>
        <span class="text-xs tabular-nums text-gray-500 dark:text-gray-400">
          {{ t('admin.usage.message.expiresAt') }} {{ formatDate(detail.expires_at) }}
        </span>
      </div>

      <div class="inline-flex rounded-md bg-gray-100 p-1 dark:bg-dark-800" role="tablist">
        <button
          v-for="tab in tabs"
          :key="tab.key"
          type="button"
          role="tab"
          :aria-selected="activeTab === tab.key"
          class="min-h-9 rounded px-3 text-sm font-medium transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
          :class="activeTab === tab.key ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white' : 'text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200'"
          @click="activeTab = tab.key"
        >
          {{ tab.label }}
        </button>
      </div>

      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex items-center gap-2 text-xs">
          <span class="inline-flex items-center rounded px-2 py-1 font-medium" :class="stateClass(currentBody.state)">
            {{ stateLabel(currentBody.state) }}
          </span>
          <span class="tabular-nums text-gray-500 dark:text-gray-400">{{ formatBytes(currentBody.raw_bytes) }}</span>
          <span v-if="currentBody.sha256" class="hidden font-mono text-gray-400 lg:inline" :title="currentBody.sha256">
            SHA256 {{ currentBody.sha256.slice(0, 12) }}…
          </span>
        </div>
        <div class="flex items-center gap-2">
          <button
            type="button"
            class="inline-flex h-10 w-10 items-center justify-center rounded text-gray-500 hover:bg-gray-100 hover:text-gray-800 disabled:cursor-not-allowed disabled:opacity-40 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-white"
            :disabled="!currentBody.body"
            :aria-label="t('admin.usage.message.copy')"
            :title="t('admin.usage.message.copy')"
            @click="copyBody"
          ><Icon name="copy" size="sm" /></button>
          <button
            type="button"
            class="inline-flex h-10 w-10 items-center justify-center rounded text-gray-500 hover:bg-gray-100 hover:text-gray-800 disabled:cursor-not-allowed disabled:opacity-40 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-white"
            :disabled="!currentBody.body"
            :aria-label="t('admin.usage.message.download')"
            :title="t('admin.usage.message.download')"
            @click="downloadBody"
          ><Icon name="download" size="sm" /></button>
        </div>
      </div>

      <pre v-if="currentBody.body" class="max-h-[55vh] min-h-72 overflow-auto whitespace-pre-wrap break-words rounded-md border border-gray-200 bg-gray-50 p-4 font-mono text-xs leading-5 text-gray-800 dark:border-dark-700 dark:bg-dark-900 dark:text-gray-200">{{ formattedBody }}</pre>
      <div v-else class="flex min-h-72 items-center justify-center rounded-md border border-dashed border-gray-300 px-6 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
        {{ unavailableMessage }}
      </div>

      <p v-if="detail.error_message" class="break-words text-xs text-rose-600 dark:text-rose-400">
        {{ detail.error_code ? `${detail.error_code}: ` : '' }}{{ detail.error_message }}
      </p>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminUsageAPI } from '@/api/admin/usage'
import type { MessageBodyState, UsageMessageDetail } from '@/api/admin/usage'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'

const props = defineProps<{ show: boolean; usageLogId: number | null }>()
const emit = defineEmits<{ 'update:show': [value: boolean] }>()
const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()
const detail = ref<UsageMessageDetail | null>(null)
const loading = ref(false)
const loadError = ref('')
const activeTab = ref<'request' | 'response'>('request')
let requestSequence = 0

const tabs = computed(() => [
  { key: 'request' as const, label: t('admin.usage.message.request') },
  { key: 'response' as const, label: t('admin.usage.message.response') }
])
const currentBody = computed(() => detail.value?.[activeTab.value] ?? { state: 'disabled' as MessageBodyState, raw_bytes: 0, stored_bytes: 0 })
const formattedBody = computed(() => {
  const body = currentBody.value.body || ''
  try { return JSON.stringify(JSON.parse(body), null, 2) } catch { return body }
})
const unavailableMessage = computed(() => t(`admin.usage.message.states.${currentBody.value.state}`))

const load = async () => {
  if (!props.usageLogId) return
  const seq = ++requestSequence
  loading.value = true
  loadError.value = ''
  try {
    const result = await adminUsageAPI.getMessageDetail(props.usageLogId, true)
    if (seq === requestSequence) detail.value = result
  } catch (error: any) {
    if (seq === requestSequence) loadError.value = error?.response?.data?.message || t('admin.usage.message.loadFailed')
  } finally {
    if (seq === requestSequence) loading.value = false
  }
}
const close = () => emit('update:show', false)
const copyBody = async () => {
  if (!currentBody.value.body) return
  if (await copyToClipboard(currentBody.value.body)) appStore.showSuccess(t('admin.usage.message.copied'))
}
const downloadBody = () => {
  if (!currentBody.value.body || !detail.value) return
  const blob = new Blob([currentBody.value.body], { type: 'application/json;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `usage-${detail.value.usage_log_id}-${activeTab.value}.txt`
  link.click()
  URL.revokeObjectURL(url)
}
const stateLabel = (state: MessageBodyState) => t(`admin.usage.message.stateLabels.${state}`)
const stateClass = (state: MessageBodyState) => ({
  available: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-300',
  partial: 'bg-amber-100 text-amber-700 dark:bg-amber-500/20 dark:text-amber-300',
  pending: 'bg-sky-100 text-sky-700 dark:bg-sky-500/20 dark:text-sky-300',
  failed: 'bg-rose-100 text-rose-700 dark:bg-rose-500/20 dark:text-rose-300',
  too_large: 'bg-orange-100 text-orange-700 dark:bg-orange-500/20 dark:text-orange-300',
  expired: 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300',
  disabled: 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
}[state])
const formatBytes = (bytes: number) => bytes < 1024 ? `${bytes} B` : bytes < 1024 * 1024 ? `${(bytes / 1024).toFixed(1)} KiB` : `${(bytes / 1024 / 1024).toFixed(2)} MiB`
const formatDate = (value: string) => new Date(value).toLocaleString()

watch(() => [props.show, props.usageLogId] as const, ([show]) => {
  requestSequence++
  detail.value = null
  loadError.value = ''
  activeTab.value = 'request'
  if (show) void load()
}, { immediate: true })
</script>
