import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import QuickNavigationDialog from '../QuickNavigationDialog.vue'

const push = vi.hoisted(() => vi.fn())
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('QuickNavigationDialog', () => {
  beforeEach(() => {
    document.body.innerHTML = '<nav class="sidebar-nav"><a href="/dashboard"><span class="sidebar-label">仪表盘</span></a><a href="/keys"><span class="sidebar-label">API 密钥</span></a><div style="display:none"><a href="/admin/channels/pricing">渠道定价</a></div></nav>'
    push.mockClear()
  })
  afterEach(() => { document.body.innerHTML = '' })

  it('opens with ⌘K, filters visible navigation, and opens the selected page with Enter', async () => {
    const wrapper = mount(QuickNavigationDialog, { props: { open: false } })
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true, bubbles: true }))
    expect(wrapper.emitted('update:open')?.[0]).toEqual([true])

    await wrapper.setProps({ open: true })
    await nextTick()
    const input = document.querySelector<HTMLInputElement>('[role="dialog"] input')!
    expect(document.activeElement).toBe(input)
    input.value = '密钥'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    expect(document.querySelectorAll('[role="option"]')).toHaveLength(1)
    expect(document.querySelector('[role="option"]')?.textContent).toContain('API 密钥')

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    expect(push).toHaveBeenCalledWith('/keys')
    wrapper.unmount()
  })

  it('ignores / while typing and closes an open dialog with Escape', async () => {
    const wrapper = mount(QuickNavigationDialog, { props: { open: false } })
    const editor = document.createElement('input')
    document.body.append(editor)
    editor.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true }))
    expect(wrapper.emitted('update:open')).toBeUndefined()

    await wrapper.setProps({ open: true })
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(wrapper.emitted('update:open')?.at(-1)).toEqual([false])
    wrapper.unmount()
  })

  it('indexes submenu pages even while their sidebar group is collapsed', async () => {
    const wrapper = mount(QuickNavigationDialog, { props: { open: false } })
    await wrapper.setProps({ open: true })
    await nextTick()
    expect(document.querySelector('[role="listbox"]')?.textContent).toContain('渠道定价')
    wrapper.unmount()
  })
})
