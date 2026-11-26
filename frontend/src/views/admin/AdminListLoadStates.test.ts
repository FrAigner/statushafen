import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import type { Component } from 'vue'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { useContentLanguages } from '../../composables/useContentLanguages'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import AdminLanguages from './AdminLanguages.vue'
import AdminIncidents from './AdminIncidents.vue'
import AdminMaintenance from './AdminMaintenance.vue'
import AdminSubscribers from './AdminSubscribers.vue'
import { fetchMock } from '../../test-support/fetchStub'

// Admin lists show a loading state before the first response
// (never the empty state), the empty state after an empty response and an
// error state on a load error. Reloading leaves existing rows
// in place.

interface ListView {
  name: string
  component: Component
  // Whether a request URL is the list request of the view.
  isList: (url: string) => boolean
  row: Record<string, unknown>
  rowText: string
  // Either the literal empty text or its i18n key.
  emptyText?: string
  emptyKey?: string
}
// Answers the list request (a response stub or a promise of one).
type Respond = () => unknown

const VIEWS: ListView[] = [
  {
    name: 'AdminLanguages', component: AdminLanguages,
    isList: (url: string) => /\/api\/languages$/.test(url),
    row: { ID: 1, Code: 'de', Name: 'Deutsch', IsActive: true }, rowText: 'Deutsch',
    emptyText: 'Noch keine Sprachen im Katalog',
  },
  {
    name: 'AdminIncidents', component: AdminIncidents,
    isList: (url: string) => /\/incidents\?limit=200$/.test(url),
    row: { ID: 1, Title: 'API down', Status: 'investigating', Components: [], Tags: [], CreatedAt: '2026-07-01T10:00:00Z' },
    rowText: 'API down', emptyKey: 'incidents.empty',
  },
  {
    name: 'AdminMaintenance', component: AdminMaintenance,
    isList: (url: string) => /\/incidents\?is_maintenance=true&limit=200$/.test(url),
    row: { ID: 1, Title: 'DB-Upgrade', Status: 'scheduled', IsMaintenance: true, Components: [], Tags: [] },
    rowText: 'DB-Upgrade', emptyKey: 'maintenance.empty',
  },
  {
    name: 'AdminSubscribers', component: AdminSubscribers,
    isList: (url: string) => /\/subscribers$/.test(url),
    row: { ID: 1, Channel: 'email', Email: 'a@x.de', SubscribedComponentIDs: '', CreatedBy: null },
    rowText: 'a@x.de', emptyKey: 'subscribers.empty',
  },
]

const jsonResponse = (body: unknown, status = 200) => ({ ok: status < 400, status, json: async () => body })

// The list request is controlled by the response function `respond`,
// all other GETs return empty lists.
function makeFetch(view: ListView, respond: Respond) {
  return vi.fn(async (url: string, options: RequestInit = {}) => {
    if ((options.method || 'GET') === 'GET' && view.isList(url)) return respond()
    return jsonResponse([])
  })
}

let activeWrapper: VueWrapper | null = null

function mountView(view: ListView, respond: Respond) {
  useTenant().setActiveTenant('1')
  global.fetch = makeFetch(view, respond) as unknown as typeof fetch
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div/>' } }] })
  activeWrapper = mount(view.component, { global: { plugins: [i18n, router], stubs: onyxFormStubs } })
  return activeWrapper
}

describe.each(VIEWS)('$name: Lade-/Leer-/Fehlerzustand', (view) => {
  const emptyText = () => view.emptyText ?? i18n.global.t(view.emptyKey!)

  beforeEach(() => {
    useAuth().logout()
    useAuth().setAuth('jwt', { email: 'a@x.de', is_global_admin: true })
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    localStorage.clear()
    useContentLanguages().catalog.value = [] // module-wide state
    vi.spyOn(console, 'error').mockImplementation(() => {})
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.restoreAllMocks()
  })

  it('zeigt solange die Anfrage läuft einen Ladeindikator statt des Leerzustands', async () => {
    const wrapper = mountView(view, () => new Promise(() => {}))
    await flushPromises()

    const status = wrapper.find('[role="status"][aria-busy="true"]')
    expect(status.exists()).toBe(true)
    expect(wrapper.text()).not.toContain(emptyText())
  })

  it('zeigt schon im ersten Render Ladezustand, weder Leer- noch Fehlerzustand', () => {
    // Without flushPromises: onMounted has started loading, the re-render is still pending.
    const wrapper = mountView(view, () => new Promise(() => {}))

    expect(wrapper.find('[role="status"][aria-busy="true"]').exists()).toBe(true)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain(emptyText())
  })

  it('zeigt nach leerer Antwort den Leerzustand ohne Ladeindikator', async () => {
    const wrapper = mountView(view, () => jsonResponse([]))
    await flushPromises()

    expect(wrapper.text()).toContain(emptyText())
    expect(wrapper.find('[role="status"][aria-busy="true"]').exists()).toBe(false)
  })

  it('zeigt nach Antwort mit Daten die Zeilen, weder Leer- noch Ladezustand', async () => {
    const wrapper = mountView(view, () => jsonResponse([view.row]))
    await flushPromises()

    expect(wrapper.text()).toContain(view.rowText)
    expect(wrapper.text()).not.toContain(emptyText())
    expect(wrapper.find('[role="status"][aria-busy="true"]').exists()).toBe(false)
  })

  it('zeigt bei einem Ladefehler den Fehlerzustand und keinen Leerzustand', async () => {
    const wrapper = mountView(view, () => jsonResponse({ error: 'kaputt' }, 500))
    await flushPromises()

    expect(wrapper.find('[role="alert"]').text()).toBe('Die Liste konnte nicht geladen werden.')
    expect(wrapper.text()).not.toContain(emptyText())
    expect(wrapper.find('[role="status"][aria-busy="true"]').exists()).toBe(false)
  })

  it('lässt beim erneuten Laden vorhandene Zeilen stehen und zeigt keinen Leerzustand', async () => {
    // Cast so that TypeScript does not narrow the variable to null; the callback below assigns it.
    let pending = null as ((value: unknown) => void) | null
    let call = 0
    const wrapper = mountView(view, () => {
      call += 1
      if (call === 1) return jsonResponse([view.row])
      return new Promise((resolve) => { pending = resolve })
    })
    await flushPromises()
    expect(wrapper.text()).toContain(view.rowText)

    // A new tenant triggers another load (the response stays open).
    useTenant().setActiveTenant('2')
    await flushPromises()

    expect(pending).not.toBeNull()
    expect(wrapper.text()).toContain(view.rowText)
    expect(wrapper.text()).not.toContain(emptyText())

    pending!(jsonResponse([]))
    await flushPromises()
    expect(wrapper.text()).toContain(emptyText())
  })

  it('zeigt nach einem Fehler und erfolgreichem Neuladen wieder normale Zeilen', async () => {
    let call = 0
    const wrapper = mountView(view, () => {
      call += 1
      return call === 1 ? jsonResponse({ error: 'kaputt' }, 500) : jsonResponse([view.row])
    })
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)

    useTenant().setActiveTenant('2')
    await flushPromises()

    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain(view.rowText)
  })
})

// Without a status page (activeTenantId = null) nothing is ever
// loaded, so no loading indicator may remain either.
describe.each(VIEWS.filter((v) => v.emptyKey))('$name: Konto ohne Statusseite', (view) => {
  beforeEach(() => {
    useAuth().logout()
    useAuth().setAuth('jwt', { email: 'a@x.de', is_global_admin: true })
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    localStorage.clear()
    useContentLanguages().catalog.value = []
    vi.spyOn(console, 'error').mockImplementation(() => {})
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.restoreAllMocks()
  })

  it('zeigt den Leerzustand statt eines Dauer-Ladeindikators und ruft nichts ab', async () => {
    global.fetch = fetchMock(async () => jsonResponse([]))
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div/>' } }] })
    activeWrapper = mount(view.component, { global: { plugins: [i18n, router], stubs: onyxFormStubs } })
    await flushPromises()

    expect(activeWrapper.find('[role="status"]').exists()).toBe(false)
    expect(activeWrapper.text()).toContain(i18n.global.t(view.emptyKey!))
    expect(global.fetch).not.toHaveBeenCalled()
  })
})
