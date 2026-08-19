import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import UsageMessageStorageSettingsDialog from '../UsageMessageStorageSettingsDialog.vue'

const { getMessageStorageSettings, updateMessageStorageSettings, showSuccess, showError } = vi.hoisted(() => ({
  getMessageStorageSettings: vi.fn(),
  updateMessageStorageSettings: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin/usage', () => ({
  adminUsageAPI: {
    getMessageStorageSettings,
    updateMessageStorageSettings
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const mountDialog = () =>
  mount(UsageMessageStorageSettingsDialog, {
    props: { show: false },
    global: {
      stubs: {
        BaseDialog: {
          props: ['show'],
          template: '<div v-if="show"><slot /><slot name="footer" /></div>'
        },
        Toggle: {
          inheritAttrs: false,
          props: ['modelValue', 'disabled'],
          emits: ['update:modelValue'],
          template:
            '<button data-testid="message-storage-enabled-toggle" :disabled="disabled" @click="$emit(\'update:modelValue\', !modelValue)" />'
        }
      }
    }
  })

describe('UsageMessageStorageSettingsDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getMessageStorageSettings.mockResolvedValue({ enabled: true, retention_days: 7 })
    updateMessageStorageSettings.mockResolvedValue({ enabled: false, retention_days: 7 })
  })

  it('loads, toggles, and saves the runtime storage setting with retention', async () => {
    const wrapper = mountDialog()
    await wrapper.setProps({ show: true })
    await flushPromises()

    await wrapper.get('[data-testid="message-storage-enabled-toggle"]').trigger('click')
    await wrapper.get('.btn-primary').trigger('click')
    await flushPromises()

    expect(updateMessageStorageSettings).toHaveBeenCalledWith(false, 7)
    expect(showSuccess).toHaveBeenCalled()
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
