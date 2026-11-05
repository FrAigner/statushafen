import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
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

// Admin-Listen zeigen vor der ersten Antwort einen Ladezustand
// (nie den Leerzustand), nach leerer Antwort den Leerzustand und bei einem
// Ladefehler einen Fehlerzustand. Ein erneutes Laden lässt vorhandene Zeilen
// stehen.

const VIEWS = [
  {
    name: 'AdminLanguages', component: AdminLanguages,
    isList: (url) => /\/api\/languages$/.test(url),
    row: { ID: 1, Code: 'de', Name: 'Deutsch', IsActive: true }, rowText: 'Deutsch',
    emptyText: 'Noch keine Sprachen im Katalog',
  },
  {
    name: 'AdminIncidents', component: AdminIncidents,
    isList: (url) => /\/incidents\?limit=200$/.test(url),
    row: { ID: 1, Title: 'API down', Status: 'investigating', Components: [], Tags: [], CreatedAt: '2026-07-01T10:00:00Z' },
    rowText: 'API down', emptyKey: 'incidents.empty',
  },
  {
    name: 'AdminMaintenance', component: AdminMaintenance,
    isList: (url) => /\/incidents\?is_maintenance=true&limit=200$/.test(url),
    row: { ID: 1, Title: 'DB-Upgrade', Status: 'scheduled', IsMaintenance: true, Components: [], Tags: [] },
    rowText: 'DB-Upgrade', emptyKey: 'maintenance.empty',
  },
  {
    name: 'AdminSubscribers', component: AdminSubscribers,
    isList: (url) => /\/subscribers$/.test(url),
    row: { ID: 1, Channel: 'email', Email: 'a@x.de', SubscribedComponentIDs: '', CreatedBy: null },
    rowText: 'a@x.de', emptyKey: 'subscribers.empty',
  },
]

const jsonResponse = (body, status = 200) => ({ ok: status < 400, status, json: async () => body })

// Der Listen-Request wird von der Antwort-Funktion `respond` gesteuert,
// alle übrigen GETs liefern leere Listen.
function makeFetch(view, respond) {
  return vi.fn(async (url, options = {}) => {
    if ((options.method || 'GET') === 'GET' && view.isList(url)) return respond()
    return jsonResponse([])
  })
}

let activeWrapper = null

function mountView(view, respond) {
  useTenant().setActiveTenant('1')
  global.fetch = makeFetch(view, respond)
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div/>' } }] })
  activeWrapper = mount(view.component, { global: { plugins: [i18n, router], stubs: onyxFormStubs } })
  return activeWrapper
}

describe.each(VIEWS)('$name: Lade-/Leer-/Fehlerzustand', (view) => {
  const emptyText = () => view.emptyText ?? i18n.global.t(view.emptyKey)

  beforeEach(() => {
    useAuth().logout()
    useAuth().setAuth('jwt', { email: 'a@x.de', is_global_admin: true })
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    localStorage.clear()
    useContentLanguages().catalog.value = [] // modulweiter Zustand
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
    // Ohne flushPromises: onMounted hat das Laden gestartet, das Re-Rendern steht noch aus.
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
    let pending = null
    let call = 0
    const wrapper = mountView(view, () => {
      call += 1
      if (call === 1) return jsonResponse([view.row])
      return new Promise((resolve) => { pending = resolve })
    })
    await flushPromises()
    expect(wrapper.text()).toContain(view.rowText)

    // Neuer Mandant löst ein erneutes Laden aus (Antwort bleibt offen).
    useTenant().setActiveTenant('2')
    await flushPromises()

    expect(pending).not.toBeNull()
    expect(wrapper.text()).toContain(view.rowText)
    expect(wrapper.text()).not.toContain(emptyText())

    pending(jsonResponse([]))
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

// Ohne Statusseite (activeTenantId = null) wird nie
// geladen, also darf auch kein Ladeindikator stehen bleiben.
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
    global.fetch = vi.fn(async () => jsonResponse([]))
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div/>' } }] })
    activeWrapper = mount(view.component, { global: { plugins: [i18n, router], stubs: onyxFormStubs } })
    await flushPromises()

    expect(activeWrapper.find('[role="status"]').exists()).toBe(false)
    expect(activeWrapper.text()).toContain(i18n.global.t(view.emptyKey))
    expect(global.fetch).not.toHaveBeenCalled()
  })
})
