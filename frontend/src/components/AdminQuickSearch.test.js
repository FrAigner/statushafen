import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../i18n'
import { useTenant } from '../composables/useTenant'
import { axe } from '../test-support/axe.js'
import { NAV_GROUPS } from '../utils/adminNav'
import AdminQuickSearch from './AdminQuickSearch.vue'

vi.mock('../utils/revealSetting', () => ({ revealSetting: vi.fn(() => Promise.resolve(null)) }))
import { revealSetting } from '../utils/revealSetting'

const Empty = { template: '<div />' }

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: Empty },
      ...NAV_GROUPS.flatMap(g => g.items).map(i => ({ path: i.to, component: Empty })),
      { path: '/docs/:section?', component: Empty },
      { path: '/admin/profile', component: Empty },
    ],
  })
}

let wrapper
async function mountSearch({ canSee = () => true } = {}) {
  const router = makeRouter()
  router.push('/admin/components')
  await router.isReady()
  wrapper = mount(AdminQuickSearch, {
    props: { canSee },
    global: { plugins: [i18n, router] },
    attachTo: document.body,
  })
  await flushPromises()
  return { wrapper, router }
}

const input = () => wrapper.find('input[role="combobox"]')
async function type(text) {
  await input().trigger('focus')
  await input().setValue(text)
  await flushPromises()
}

describe('AdminQuickSearch', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    useTenant().setActiveTenant(7)
    vi.mocked(revealSetting).mockClear()
  })
  afterEach(() => {
    wrapper?.unmount()
    useTenant().setActiveTenant(null)
  })

  it('renders an accessible, labelled combobox that is collapsed at first', async () => {
    await mountSearch()
    const el = input()
    expect(el.attributes('aria-label')).toBe(i18n.global.t('quickSearch.label'))
    expect(el.attributes('aria-expanded')).toBe('false')
    expect(el.attributes('aria-autocomplete')).toBe('list')
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  it('shows grouped results while typing', async () => {
    await mountSearch()
    await type('Antwortadresse')
    const listbox = wrapper.find('[role="listbox"]')
    expect(listbox.exists()).toBe(true)
    expect(input().attributes('aria-expanded')).toBe('true')
    expect(input().attributes('aria-controls')).toBe(listbox.attributes('id'))
    const group = wrapper.find('[role="group"]')
    expect(wrapper.find(`#${group.attributes('aria-labelledby')}`).text()).toBe(i18n.global.t('quickSearch.groups.settings'))
    const option = wrapper.find('[role="option"]')
    expect(option.text()).toContain(i18n.global.t('statusPages.fieldNotificationFrom'))
    expect(option.text()).toContain(i18n.global.t('adminNav.items.pages'))
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  it('moves the active option with the arrow keys', async () => {
    await mountSearch()
    await type('statusseite')
    const options = wrapper.findAll('[role="option"]')
    expect(options.length).toBeGreaterThan(1)
    expect(input().attributes('aria-activedescendant')).toBe(options[0].attributes('id'))
    expect(options[0].attributes('aria-selected')).toBe('true')
    await input().trigger('keydown', { key: 'ArrowDown' })
    expect(input().attributes('aria-activedescendant')).toBe(options[1].attributes('id'))
    await input().trigger('keydown', { key: 'ArrowUp' })
    expect(input().attributes('aria-activedescendant')).toBe(options[0].attributes('id'))
  })

  it('Enter opens the setting: navigates (with the edit form of the active page) and reveals the field', async () => {
    const { router } = await mountSearch()
    await type('Antwortadresse')
    await input().trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/pages')
    expect(router.currentRoute.value.query).toEqual({ edit: '7' })
    expect(revealSetting).toHaveBeenCalledWith('page-notification-from', { fallbackId: undefined })
    expect(input().element.value).toBe('')
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
  })

  it('a click on a page result navigates there', async () => {
    const { router } = await mountSearch()
    await type('Wartung')
    const option = wrapper.findAll('[role="option"]').find(o => o.text().includes(i18n.global.t('adminNav.items.maintenance')))
    await option.trigger('mousedown')
    await option.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/maintenance')
    expect(revealSetting).not.toHaveBeenCalled()
  })

  it('Escape closes the list, a second Escape clears the field', async () => {
    await mountSearch()
    await type('privat')
    await input().trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    expect(input().element.value).toBe('privat')
    await input().trigger('keydown', { key: 'Escape' })
    expect(input().element.value).toBe('')
  })

  it('announces when nothing matches', async () => {
    await mountSearch()
    await type('xyzzy-nichts')
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    expect(wrapper.find('.quick-search__status[aria-live="polite"]').text()).toContain(i18n.global.t('quickSearch.noResults', { query: 'xyzzy-nichts' }))
  })

  it('only lists what the user may see', async () => {
    await mountSearch({ canSee: (item) => item.resource !== 'sso' })
    await type('Discovery')
    expect(wrapper.findAll('[role="option"][data-kind="setting"]')).toHaveLength(0)
  })

  it('Ctrl+K and Cmd+K focus the field from anywhere', async () => {
    await mountSearch()
    document.body.focus()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, bubbles: true }))
    expect(document.activeElement).toBe(input().element)
    input().element.blur()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true, bubbles: true }))
    expect(document.activeElement).toBe(input().element)
  })

  it('"/" focuses the field, but not while typing in another field', async () => {
    await mountSearch()
    const other = document.createElement('input')
    document.body.appendChild(other)
    other.focus()
    other.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true }))
    expect(document.activeElement).toBe(other)
    other.blur()
    document.body.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true }))
    expect(document.activeElement).toBe(input().element)
    other.remove()
  })

  it('has a toggle button for small screens that opens the field', async () => {
    await mountSearch()
    const toggle = wrapper.find('button.quick-search__toggle')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(toggle.attributes('aria-label')).toBe(i18n.global.t('quickSearch.open'))
    await toggle.trigger('click')
    await flushPromises()
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(wrapper.find('.quick-search').classes()).toContain('is-open')
    expect(document.activeElement).toBe(input().element)
  })

  it('also finds built-in docs sections', async () => {
    await mountSearch()
    await type('Selbstüberwachung')
    // The docs index is imported lazily on first focus.
    const labels = () => wrapper.findAll('[role="group"]').map(g => wrapper.find(`#${g.attributes('aria-labelledby')}`).text())
    await vi.waitFor(() => expect(labels()).toContain(i18n.global.t('quickSearch.groups.docs')), { timeout: 5000 })
  })
})
