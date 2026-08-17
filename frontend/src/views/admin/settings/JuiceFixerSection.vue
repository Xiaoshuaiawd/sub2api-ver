<template>
  <div class="space-y-6">
    <!-- Juice value fixer rules -->
    <div class="card p-6">
      <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">
            {{ t('admin.settings.juiceFixer.title') }}
          </h3>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.settings.juiceFixer.description') }}
          </p>
        </div>
        <label class="inline-flex cursor-pointer items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
          <input v-model="form.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
          <span>{{ t('admin.settings.juiceFixer.enabled') }}</span>
        </label>
      </div>

      <div v-if="loading" class="py-4 text-center text-sm text-gray-500">
        {{ t('common.loading') }}
      </div>
      <template v-else>
        <div class="space-y-2">
          <div
            v-for="(rule, index) in form.rules"
            :key="rule.id"
            class="flex flex-wrap items-center gap-2 rounded-lg border border-gray-200 p-2 dark:border-dark-600"
          >
            <div class="min-w-[180px] flex-1">
              <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
                {{ t('admin.settings.juiceFixer.model') }}
              </label>
              <input
                v-model.trim="rule.model"
                class="input w-full"
                :placeholder="t('admin.settings.juiceFixer.modelPlaceholder')"
              />
            </div>
            <div class="min-w-[140px] flex-1">
              <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
                {{ t('admin.settings.juiceFixer.reasoningEffort') }}
              </label>
              <input
                v-model.trim="rule.reasoning_effort"
                class="input w-full"
                :placeholder="t('admin.settings.juiceFixer.reasoningEffortPlaceholder')"
              />
            </div>
            <div class="w-28">
              <label class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
                {{ t('admin.settings.juiceFixer.value') }}
              </label>
              <input
                v-model.number="rule.value"
                type="number"
                min="0"
                class="input w-full"
              />
            </div>
            <button
              type="button"
              class="btn btn-secondary btn-sm mt-5"
              :disabled="form.rules.length <= 1"
              :title="t('admin.settings.juiceFixer.removeRule')"
              @click="removeRule(index)"
            >
              <Icon name="trash" size="sm" />
            </button>
          </div>
        </div>

        <div class="mt-4 flex flex-wrap items-center gap-2">
          <button type="button" class="btn btn-secondary btn-sm" @click="addRule">
            <Icon name="plus" size="sm" />
            {{ t('admin.settings.juiceFixer.addRule') }}
          </button>
          <button type="button" class="btn btn-primary btn-sm" :disabled="saving" @click="save">
            {{ saving ? t('common.loading') : t('common.save') }}
          </button>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { adminAPI } from "@/api/admin";
import type { JuiceFixerRule } from "@/api/admin/juiceFixer";
import Icon from "@/components/icons/Icon.vue";
import { useAppStore } from "@/stores";
import { extractApiErrorMessage } from "@/utils/apiError";

type EditableRule = JuiceFixerRule & { id: string };

const { t } = useI18n();
const appStore = useAppStore();

const loading = ref(false);
const saving = ref(false);
const form = ref<{ enabled: boolean; rules: EditableRule[] }>({
  enabled: false,
  rules: [],
});

function createRuleId(): string {
  return (
    globalThis.crypto?.randomUUID?.() ??
    `${Date.now()}-${Math.random().toString(36).slice(2)}`
  );
}

function addRule(): void {
  form.value.rules.push({
    id: createRuleId(),
    model: "",
    reasoning_effort: "",
    value: 0,
  });
}

function removeRule(index: number): void {
  if (form.value.rules.length <= 1) return;
  form.value.rules.splice(index, 1);
}

function toPayload(): { enabled: boolean; rules: JuiceFixerRule[] } {
  return {
    enabled: form.value.enabled,
    rules: form.value.rules.map((rule) => ({
      model: rule.model,
      reasoning_effort: rule.reasoning_effort,
      value: Number.isFinite(rule.value) ? rule.value : 0,
    })),
  };
}

async function load(): Promise<void> {
  loading.value = true;
  try {
    const config = await adminAPI.juiceFixer.getJuiceFixerConfig();
    form.value = {
      enabled: config.enabled,
      rules: (config.rules || []).map((rule) => ({
        ...rule,
        id: createRuleId(),
      })),
    };
    if (form.value.rules.length === 0) {
      addRule();
    }
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t("common.error")));
  } finally {
    loading.value = false;
  }
}

async function save(): Promise<void> {
  saving.value = true;
  try {
    const config = await adminAPI.juiceFixer.updateJuiceFixerConfig(toPayload());
    form.value = {
      enabled: config.enabled,
      rules: (config.rules || []).map((rule) => ({
        ...rule,
        id: createRuleId(),
      })),
    };
    if (form.value.rules.length === 0) {
      addRule();
    }
    appStore.showSuccess(t("admin.settings.juiceFixer.saveSuccess"));
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t("common.error")));
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>
