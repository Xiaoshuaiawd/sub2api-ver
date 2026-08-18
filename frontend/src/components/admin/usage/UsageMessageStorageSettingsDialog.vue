<template>
  <BaseDialog
    :show="show"
    :title="t('admin.usage.message.settingsTitle')"
    width="narrow"
    @close="close"
  >
    <div class="space-y-4">
      <div>
        <label class="input-label" for="message-retention-days">
          {{ t('admin.usage.message.retentionDays') }}
        </label>
        <input
          id="message-retention-days"
          v-model.number="retentionDays"
          class="input"
          type="number"
          min="1"
          max="30"
          step="1"
          :disabled="loading || saving"
        />
        <p class="input-hint">{{ t('admin.usage.message.retentionHint') }}</p>
      </div>

      <div
        v-if="loadError"
        class="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-200"
      >
        {{ t('admin.usage.message.settingsLoadFailed') }}
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="saving" @click="close">
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="loading || saving || !isValid"
          @click="save"
        >
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminUsageAPI } from '@/api/admin/usage'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAppStore } from '@/stores/app'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: []; saved: [retentionDays: number] }>()

const { t } = useI18n()
const appStore = useAppStore()
const retentionDays = ref(7)
const loading = ref(false)
const saving = ref(false)
const loadError = ref(false)

const isValid = computed(
  () => Number.isInteger(retentionDays.value) && retentionDays.value >= 1 && retentionDays.value <= 30
)

const load = async () => {
  loading.value = true
  loadError.value = false
  try {
    const settings = await adminUsageAPI.getMessageStorageSettings()
    retentionDays.value = settings.retention_days
  } catch {
    loadError.value = true
  } finally {
    loading.value = false
  }
}

const save = async () => {
  if (!isValid.value || saving.value) return
  saving.value = true
  try {
    const settings = await adminUsageAPI.updateMessageStorageSettings(retentionDays.value)
    retentionDays.value = settings.retention_days
    appStore.showSuccess(t('admin.usage.message.settingsSaved'))
    emit('saved', settings.retention_days)
    emit('close')
  } catch {
    appStore.showError(t('admin.usage.message.settingsSaveFailed'))
  } finally {
    saving.value = false
  }
}

const close = () => {
  if (!saving.value) emit('close')
}

watch(
  () => props.show,
  (show) => {
    if (show) void load()
  }
)
</script>
