import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { useTenant } from '../../composables/useTenant'
import { useCurrentPageSlug } from '../../composables/useCurrentPageSlug'
import { usePublicPageLanguages } from '../../composables/usePublicPageLanguages'
import StatusPage from './StatusPage.vue'

// Preview mode of the public status page (used by the admin edit form):
// draft design values override the saved ones, and the page has no side effects.

function dashboard() {
  return {
    page: { ID: 1, Name: 'Acme', LogoURL: '', PrimaryColor: '', SecondaryColor: '', HeaderStyle: 'classic', PageLayout: 'standard' },
    components: [{ ID: 10, Name: 'API', Status: 'operational', Tags: [], Children: [] }],
    incidents: [],
    notices: [],
    languages: [{ code: 'de', name: 'Deutsch', is_default: true }],
  }
}

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

function routedFetch() {
  return vi.fn(async (url) => {
    if (url.includes('/availability')) return jsonResponse({}, 404)
    if (url.includes('/api/me/pages')) return jsonResponse([{ ID: 1 }])
    if (url.includes('/translations')) return jsonResponse({})
    return jsonResponse(dashboard())
  })
}
const callsTo = (fetchMock, part) => fetchMock.mock.calls.filter(([url]) => url.includes(part))

let wrapper = null
async function mountPreview(preview, fetchMock = routedFetch()) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { template: '<div/>' } }] })
  router.push('/admin/pages')
  await router.isReady()
  global.fetch = fetchMock
  wrapper = mount(StatusPage, {
    props: { slug: 'acme', preview },
    global: {
      plugins: [i18n, router],
      stubs: {
        ComponentTreeNode: { props: ['node', 'depth'], template: '<div class="component-tree-node-stub">{{ node.Name }}</div>' },
        PageLayoutList: { props: ['components', 'timelineIncidents', 'availability'], template: '<div class="page-layout-list-stub" />' },
        SubscriptionManager: { template: '<div class="subscription-manager-stub" />' },
      },
    },
  })
  await flushPromises()
  return { wrapper, fetchMock }
}

describe('StatusPage preview mode', () => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    useCurrentPageSlug().currentPageSlug.value = null
    usePublicPageLanguages().clearPageLanguages()
    i18n.global.locale.value = 'de'
  })
  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('renders the draft design values instead of the saved ones', async () => {
    await mountPreview({ LogoURL: 'https://acme.example/logo.png', PrimaryColor: '#0b5394', HeaderStyle: 'centered', PageLayout: 'liste' })

    expect(wrapper.find('header.sp-head').classes()).toContain('status-header--centered')
    expect(wrapper.find('img.status-header-logo').attributes('src')).toBe('https://acme.example/logo.png')
    expect(wrapper.find('.page-layout-list-stub').exists()).toBe(true)
    expect(wrapper.find('.content-area').attributes('style')).toContain('--primary: #0b5394')
  })

  it('follows changes of the draft design without reloading the page', async () => {
    const { fetchMock } = await mountPreview({ HeaderStyle: 'classic', PageLayout: 'standard' })
    expect(wrapper.find('.page-layout-list-stub').exists()).toBe(false)

    await wrapper.setProps({ preview: { HeaderStyle: 'minimal', PageLayout: 'liste' } })
    await flushPromises()

    expect(wrapper.find('header.sp-head').classes()).toContain('status-header--minimal')
    expect(wrapper.find('.page-layout-list-stub').exists()).toBe(true)
    expect(callsTo(fetchMock, '/dashboard')).toHaveLength(1)
    // The list layout shows per-component bars, so availability is fetched again with components.
    expect(callsTo(fetchMock, 'include=components')).toHaveLength(1)
  })

  it('keeps the saved values for fields the draft does not contain', async () => {
    await mountPreview({})
    expect(wrapper.find('header.sp-head').classes()).not.toContain('status-header--centered')
    expect(wrapper.find('.component-tree-node-stub').exists()).toBe(true)
  })

  it('is read-only: inert, without subscription management', async () => {
    await mountPreview({})
    expect(wrapper.find('.content-area').attributes('inert')).toBeDefined()
    expect(wrapper.find('.subscription-manager-stub').exists()).toBe(false)
  })

  it('does not poll', async () => {
    vi.useFakeTimers()
    const { fetchMock } = await mountPreview({})
    await vi.advanceTimersByTimeAsync(120000)
    expect(callsTo(fetchMock, '/dashboard')).toHaveLength(1)
  })

  it('leaves the global page state of the surrounding admin untouched', async () => {
    useAuth().setAuth('jwt-1', { email: 'a@b.de', is_global_admin: true })
    useTenant().setActiveTenant(7)
    const { fetchMock } = await mountPreview({})

    expect(useCurrentPageSlug().currentPageSlug.value).toBeNull()
    expect(usePublicPageLanguages().pageLanguages.value).toEqual([])
    expect(useTenant().activeTenantId.value).toBe(7)
    expect(callsTo(fetchMock, '/api/me/pages')).toHaveLength(0)
  })
})
