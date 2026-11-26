import { describe, it, expect, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../i18n'
import { useAuth } from '../composables/useAuth'
import { useTenant } from '../composables/useTenant'
import { NAV_GROUPS } from '../utils/adminNav'
import AdminLayout from './AdminLayout.vue'

const Empty = { template: '<div />' }

describe('AdminLayout quick search', () => {
  let wrapper: VueWrapper | undefined
  afterEach(() => { wrapper?.unmount(); useAuth().logout(); useTenant().setMyPages([]) })

  it('puts the quick search into the top bar between tenant switch and the right-hand buttons', async () => {
    i18n.global.locale.value = 'de'
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => [] })
    useAuth().setAuth('jwt-1', { email: 'admin@x.de', is_global_admin: true })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: Empty },
        { path: '/admin/profile', component: Empty },
        ...NAV_GROUPS.flatMap(g => g.items).map(i => ({ path: i.to, component: Empty })),
      ],
    })
    router.push('/admin/components')
    await router.isReady()
    wrapper = mount(AdminLayout, { global: { plugins: [i18n, router] } })
    await flushPromises()
    const kids = Array.from(wrapper.find('.topbar-main').element.children).map(el => el.classList[0])
    expect(kids.indexOf('quick-search')).toBeGreaterThan(kids.indexOf('tenant-switch'))
    expect(kids.indexOf('quick-search')).toBeLessThan(kids.indexOf('topbar-right'))
    // Only visible nav items are searchable (same rule as the nav).
    await wrapper.find('.quick-search input').trigger('focus')
    await wrapper.find('.quick-search input').setValue('Systemeinstellungen')
    await flushPromises()
    expect(wrapper.find('.quick-search [role="option"]').exists()).toBe(true)
  })
})
