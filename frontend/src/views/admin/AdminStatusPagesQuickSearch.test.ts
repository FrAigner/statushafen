import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { fetchMock } from '../../test-support/fetchStub'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import AdminStatusPages from './AdminStatusPages.vue'

// Quick search deep link: /admin/pages?edit=<id> opens the edit form of that page.
const pages = [
  { ID: 1, Name: 'Acme Corp', Slug: 'acme', IsPrivate: false },
  { ID: 2, Name: 'Beta Internal', Slug: 'beta', IsPrivate: true },
]
const json = (body: unknown) => ({ ok: true, status: 200, json: async () => body })

let wrapper: VueWrapper
async function mountAt(path: string) {
  useAuth().setAuth('jwt-1', { email: 'a@b.de', is_global_admin: true })
  global.fetch = fetchMock(async (url) => (/\/api\/pages$/.test(String(url)) ? json(pages) : json([])))
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/pages', component: AdminStatusPages }] })
  router.push(path)
  await router.isReady()
  wrapper = mount({ template: '<router-view />' }, { global: { plugins: [i18n, router] } })
  await flushPromises()
  return router
}

describe('AdminStatusPages quick search deep link', () => {
  beforeEach(() => { i18n.global.locale.value = 'de' })
  afterEach(() => { wrapper?.unmount(); useAuth().logout() })

  it('opens the edit form of the page named in ?edit and removes the parameter', async () => {
    const router = await mountAt('/admin/pages?edit=2')
    expect(wrapper.find<HTMLInputElement>('#page-name').element.value).toBe('Beta Internal')
    expect(wrapper.find<HTMLInputElement>('#isPrivate').element.checked).toBe(true)
    expect(router.currentRoute.value.query).toEqual({})
  })

  it('reacts to ?edit while the page is already open', async () => {
    const router = await mountAt('/admin/pages')
    expect(wrapper.find('#page-name').exists()).toBe(false)
    await router.push('/admin/pages?edit=1')
    await flushPromises()
    expect(wrapper.find<HTMLInputElement>('#page-name').element.value).toBe('Acme Corp')
  })

  it('ignores an unknown page id', async () => {
    await mountAt('/admin/pages?edit=99')
    expect(wrapper.find('#page-name').exists()).toBe(false)
  })

  it('gives the sections the quick search jumps to stable ids', async () => {
    await mountAt('/admin/pages?edit=1')
    for (const id of ['page-layout', 'page-webhook', 'custom-domain-input', 'page-notification-from']) {
      expect(wrapper.find(`#${id}`).exists(), id).toBe(true)
    }
  })
})
