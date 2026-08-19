import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import Toggle from '../Toggle.vue'

describe('Toggle', () => {
  it('does not emit an update when disabled', async () => {
    const wrapper = mount(Toggle, {
      props: {
        modelValue: true,
        disabled: true
      }
    })

    await wrapper.get('button').trigger('click')

    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    expect(wrapper.get('button').attributes('aria-disabled')).toBe('true')
    expect(wrapper.get('button').classes()).toContain('cursor-not-allowed')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
})
