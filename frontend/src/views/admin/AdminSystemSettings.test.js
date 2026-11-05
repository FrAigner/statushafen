import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { onyxFormStubs, OnyxCheckboxStub } from '../../test-support/onyxStubs'
import AdminSystemSettings from './AdminSystemSettings.vue'
import { axe } from '../../test-support/axe.js'

// Component-Tests für die Systemeinstellungen - beide
// Schalter rendern, laden den Stand und speichern nur Änderungen (gemockter
// fetch).

function jsonResponse(body, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

const ALL_ON = { orders_enabled: true, email_enabled: true, selfmonitor_enabled: true }

const stubs = { ...onyxFormStubs, OnyxCheckbox: OnyxCheckboxStub }

let activeWrapper = null

async function mountView({ fetchImpl, globalAdmin = true } = {}) {
  if (globalAdmin) {
    useAuth().setAuth('jwt', { email: 'admin@x.de', is_global_admin: true })
  } else {
    useAuth().setAuth('jwt', { email: 'user@x.de', is_global_admin: false })
  }
  global.fetch = fetchImpl || vi.fn(async () => jsonResponse(ALL_ON))

  const wrapper = mount(AdminSystemSettings, {
    global: { plugins: [i18n], stubs },
  })
  await flushPromises()
  activeWrapper = wrapper
  return wrapper
}

// fetchWithPut liefert ALL_ON für GET und putBody (bzw. Fehler) für PUT.
function fetchWithPut(putResponse) {
  return vi.fn(async (url, options = {}) => {
    if ((options.method || 'GET') === 'PUT') return putResponse
    return jsonResponse(ALL_ON)
  })
}

const checkbox = (wrapper, field) => wrapper.find(`[data-setting="${field}"] input[type="checkbox"]`)
const putCalls = (fetchMock) => fetchMock.mock.calls.filter(([, opts]) => opts?.method === 'PUT')

async function clickSave(wrapper) {
  await wrapper.find('button.save-settings').trigger('click')
  await flushPromises()
}

describe('AdminSystemSettings', () => {
  beforeEach(() => {
    useAuth().logout()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    i18n.global.locale.value = 'de'
  })

  afterEach(() => {
    activeWrapper?.unmount()
    activeWrapper = null
    vi.restoreAllMocks()
  })

  it('lädt die Einstellungen und spiegelt beide Schalter in die Checkboxen', async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ orders_enabled: false, email_enabled: true }))
    const wrapper = await mountView({ fetchImpl: fetchMock })

    expect(fetchMock).toHaveBeenCalledWith('/api/settings', expect.anything())
    expect(checkbox(wrapper, 'orders_enabled').element.checked).toBe(false)
    expect(checkbox(wrapper, 'email_enabled').element.checked).toBe(true)
    expect(wrapper.text()).toContain('E-Mail-Versand aktiviert')
  })

  it('speichert nur den umgeschalteten E-Mail-Schalter und zeigt eine Erfolgsmeldung', async () => {
    const fetchMock = fetchWithPut(jsonResponse({ orders_enabled: true, email_enabled: false }))
    const wrapper = await mountView({ fetchImpl: fetchMock })

    await checkbox(wrapper, 'email_enabled').setValue(false)
    await clickSave(wrapper)

    const puts = putCalls(fetchMock)
    expect(puts).toHaveLength(1)
    expect(JSON.parse(puts[0][1].body)).toEqual({ email_enabled: false })
    expect(wrapper.text()).toContain('Einstellungen gespeichert.')
    expect(checkbox(wrapper, 'email_enabled').element.checked).toBe(false)
    expect(checkbox(wrapper, 'orders_enabled').element.checked).toBe(true)

    // Dismiss-Button schließt die Meldung wieder.
    await wrapper.find('.alert-close').trigger('click')
    expect(wrapper.find('.alert').exists()).toBe(false)
  })

  it('speichert den umgeschalteten Bestell-Schalter', async () => {
    const fetchMock = fetchWithPut(jsonResponse({ orders_enabled: false, email_enabled: true }))
    const wrapper = await mountView({ fetchImpl: fetchMock })

    await checkbox(wrapper, 'orders_enabled').setValue(false)
    await clickSave(wrapper)

    expect(JSON.parse(putCalls(fetchMock)[0][1].body)).toEqual({ orders_enabled: false })
    expect(checkbox(wrapper, 'orders_enabled').element.checked).toBe(false)
  })

  it('zeigt und speichert den Schalter der Selbstüberwachung', async () => {
    const fetchMock = fetchWithPut(jsonResponse({ ...ALL_ON, selfmonitor_enabled: false }))
    const wrapper = await mountView({ fetchImpl: fetchMock })

    expect(checkbox(wrapper, 'selfmonitor_enabled').element.checked).toBe(true)
    expect(wrapper.text()).toContain('Selbstüberwachung aktiviert')
    await checkbox(wrapper, 'selfmonitor_enabled').setValue(false)
    await clickSave(wrapper)

    expect(JSON.parse(putCalls(fetchMock)[0][1].body)).toEqual({ selfmonitor_enabled: false })
    expect(checkbox(wrapper, 'selfmonitor_enabled').element.checked).toBe(false)
  })

  it('schickt ohne Änderung keinen PUT', async () => {
    const fetchMock = fetchWithPut(jsonResponse(ALL_ON))
    const wrapper = await mountView({ fetchImpl: fetchMock })

    await clickSave(wrapper)

    expect(putCalls(fetchMock)).toHaveLength(0)
    expect(wrapper.text()).toContain('Einstellungen gespeichert.')
  })

  it('zeigt eine Fehlermeldung, wenn das Speichern scheitert', async () => {
    const fetchMock = fetchWithPut(jsonResponse({ error: 'kaputt' }, 500))
    const wrapper = await mountView({ fetchImpl: fetchMock })

    await checkbox(wrapper, 'email_enabled').setValue(false)
    await clickSave(wrapper)

    expect(wrapper.find('.alert-error').exists()).toBe(true)
    expect(wrapper.find('button.save-settings').element.disabled).toBe(false)
  })

  it('zeigt eine Fehlermeldung, wenn das Laden scheitert', async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ error: 'kaputt' }, 500))
    const wrapper = await mountView({ fetchImpl: fetchMock })

    expect(wrapper.find('.alert-error').exists()).toBe(true)
  })

  it('zeigt Nicht-Global-Admins einen Hinweis statt der Schalter und lädt nichts', async () => {
    const fetchMock = vi.fn(async () => jsonResponse(ALL_ON))
    const wrapper = await mountView({ fetchImpl: fetchMock, globalAdmin: false })

    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.find('input[type="checkbox"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Nur globale Administratoren')
  })

  it('hat keine axe-Funde', async () => {
    const wrapper = await mountView()
    expect(await axe(wrapper)).toHaveNoViolations()
  })

  // Richtung C: Brotkrumen und Einstellungen in einer Karte; die
  // Abschnitte stehen als h4 unter dem Kartentitel (h3), die Seite hat nur die h2.
  it('zeigt Brotkrumen und die Schalter in einer Karte mit Titel', async () => {
    const wrapper = await mountView()

    expect(wrapper.find('.page-head__crumbs').text()).toContain('Administration')
    expect(wrapper.find('.page-head__crumbs [aria-current="page"]').text()).toBe('Systemeinstellungen')
    expect(wrapper.find('section.comp-card h3').text()).toBe('Plattform')
    expect(wrapper.findAll('section.comp-card h4')).toHaveLength(4)
    expect(wrapper.findAll('.setting h3')).toHaveLength(0)
  })
})
