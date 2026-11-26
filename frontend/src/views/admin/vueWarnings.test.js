// The admin pages with OnyxSelect produced Vue warnings on every render
// ("Missing required prop: listLabel", "Invalid prop: type check failed for
// prop label"), because sit-onyx requires the listLabel prop. All other tests
// stub OnyxSelect and never see it - this test mounts the REAL
// sit-onyx components and fails on every Vue warning.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { useTenant } from '../../composables/useTenant'
import AdminNotificationTemplates from './AdminNotificationTemplates.vue'
import AdminSubscribers from './AdminSubscribers.vue'
import ProfileView from './ProfileView.vue'

// Otherwise Vitest resolves sit-onyx via the "node" condition (dist/index.js) -
// this build variant does NOT declare listLabel as a required prop, so the
// warning would stay invisible. The browser (Vite) loads dist/index.esm-bundler.js,
// so force exactly this variant here.
vi.mock('sit-onyx', async () => await import('sit-onyx/dist/index.esm-bundler.js'))

const json = (body) => ({ ok: true, status: 200, json: async () => body })

describe('Keine Vue-Warnungen mit echten sit-onyx-Komponenten', () => {
  let warn
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    useAuth().setAuth('jwt-1', { email: 'a@b.de', is_global_admin: true })
    useTenant().setActiveTenant('1')
    global.fetch = vi.fn(async (url) => json(/\/me\/sso/.test(String(url)) || /sso/.test(String(url)) ? { identities: [], linkable: [] } : []))
    warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
  })
  afterEach(() => {
    vi.restoreAllMocks()
    useAuth().logout()
    useTenant().setActiveTenant(null)
  })

  const vueWarnings = () => warn.mock.calls.map(c => String(c[0])).filter(m => m.includes('[Vue warn]'))

  it.each([
    ['AdminNotificationTemplates', AdminNotificationTemplates],
    ['ProfileView', ProfileView],
  ])('%s mountet ohne Vue-Warnungen', async (_name, comp) => {
    const wrapper = mount(comp, { global: { plugins: [i18n], stubs: { RouterLink: true } } })
    await flushPromises()
    expect(vueWarnings()).toEqual([])
    wrapper.unmount()
  })

  // The component selection (multiple) is in the form and is only rendered
  // when it is opened.
  it('AdminSubscribers mountet mit geöffnetem Formular ohne Vue-Warnungen', async () => {
    const wrapper = mount(AdminSubscribers, { global: { plugins: [i18n], stubs: { RouterLink: true } } })
    await flushPromises()
    await wrapper.find('.page-head__actions button').trigger('click')
    await flushPromises()
    expect(wrapper.find('.edit-form').exists()).toBe(true)
    expect(vueWarnings()).toEqual([])
    wrapper.unmount()
  })
})
