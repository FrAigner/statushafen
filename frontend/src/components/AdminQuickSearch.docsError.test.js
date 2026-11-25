import { describe, it, expect, vi, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../i18n'
import AdminQuickSearch from './AdminQuickSearch.vue'

// A failing docs chunk must not break the search: pages and settings still work.
vi.mock('../utils/docsSearchIndex', () => { throw new Error('chunk failed') })

describe('AdminQuickSearch without docs index', () => {
  let wrapper
  afterEach(() => wrapper?.unmount())

  it('logs the load error and keeps searching pages and settings', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    i18n.global.locale.value = 'de'
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { template: '<div />' } }] })
    router.push('/admin/components')
    await router.isReady()
    wrapper = mount(AdminQuickSearch, { props: { canSee: () => true }, global: { plugins: [i18n, router] } })
    const field = wrapper.find('input[role="combobox"]')
    await field.trigger('focus')
    await field.setValue('Antwortadresse')
    await vi.waitFor(() => expect(errorSpy).toHaveBeenCalledWith('Failed to load docs search index', expect.any(Error)))
    expect(wrapper.find('[role="option"][data-kind="setting"]').exists()).toBe(true)
    errorSpy.mockRestore()
    await flushPromises()
  })
})
