import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { setAuthAsPageRole } from '../../test-support/pageRole'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import { componentTreeFixture } from '../../test-support/componentTree'
import AdminIncidents from './AdminIncidents.vue'
import { fieldByLabel } from '../../test-support/fieldByLabel.js'

// Sprachumschalter im Vorfall-Formular.

const json = (body, status = 200) => ({ ok: status < 400, status, json: async () => body })
const catalog = [{ Code: 'de', Name: 'Deutsch' }, { Code: 'en', Name: 'English' }, { Code: 'fr', Name: 'Français' }]
const pageLangs = [
  { LanguageCode: 'de', IsDefault: true },
  { LanguageCode: 'en', IsDefault: false },
  { LanguageCode: 'fr', IsDefault: false },
]
const incident = { ID: 1, Title: 'API down', Message: 'API antwortet nicht', Status: 'investigating', Components: [{ ID: 10, Name: 'API' }], Tags: [], CreatedAt: '2026-07-01T10:00:00Z' }

function router({ languages = pageLangs, languagesOk = true, translations = [], catalogBody = catalog } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (method === 'GET' && /\/incident-templates$/.test(url)) return json([])
    if (method === 'GET' && /\/pages\/\d+\/incidents\?limit=200$/.test(url)) return json([incident])
    if (method === 'GET' && /\/components$/.test(url)) return json(componentTreeFixture())
    if (method === 'GET' && /\/api\/pages\/\d+\/tags$/.test(url)) return json([])
    if (method === 'GET' && /\/api\/tags/.test(url)) return json([])
    if (method === 'GET' && url === '/api/languages') return json(catalogBody)
    if (method === 'GET' && /\/pages\/\d+\/languages$/.test(url)) return languagesOk ? json(languages) : json({ error: 'x' }, 500)
    if (method === 'GET' && /\/incidents\/\d+\/translations\/all$/.test(url)) return json(translations)
    if (method === 'POST' && /\/incidents$/.test(url)) return json({ ID: 99 })
    if (method === 'PUT' && /\/incidents\/\d+$/.test(url)) return json({ ID: 1 })
    if (method === 'PUT' && /\/incidents\/\d+\/translations\/[a-z]+$/.test(url)) return json({})
    throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
  })
}

let wrapper = null
async function mountView(fetchImpl, perms = 'incidents:create,incidents:update') {
  setAuthAsPageRole('jwt-1', { permissions: perms })
  const r = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { template: '<div/>' } }] })
  r.push('/')
  await r.isReady()
  useTenant().setActiveTenant('1')
  global.fetch = fetchImpl
  wrapper = mount(AdminIncidents, { global: { plugins: [i18n, r], stubs: onyxFormStubs } })
  await flushPromises()
  return wrapper
}
const openNew = async () => { await wrapper.find('.page-head button').trigger('click'); await flushPromises() }
const tab = (text) => wrapper.findAll('[data-test="lang-switch"] button:not([data-test^="lang-translate"])').find(b => b.text().includes(text))
const calls = (fetchMock, method, re) => fetchMock.mock.calls.filter(([u, o]) => (o?.method || 'GET') === method && re.test(u))
const fillDefault = async () => {
  await fieldByLabel(wrapper, 'Titel').setValue('Neuer Vorfall')
  await fieldByLabel(wrapper, 'Nachricht / Details').setValue('Details')
  await wrapper.find('.onyx-select-stub-option input[type="checkbox"]').setValue(true)
}

describe('AdminIncidents: Sprachumschalter', () => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    localStorage.clear()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    i18n.global.locale.value = 'de'
  })
  afterEach(() => { wrapper?.unmount(); wrapper = null; vi.restoreAllMocks() })

  it('zeigt den Umschalter mit Standardsprache zuerst und Stand je Sprache', async () => {
    await mountView(router())
    await openNew()
    const labels = wrapper.findAll('[data-test="lang-switch"] button:not([data-test^="lang-translate"])').map(b => b.text())
    expect(labels).toEqual(['🇩🇪 Deutsch (Standard)', '🇬🇧 English · leer', '🇫🇷 Français · leer'])
  })

  it('zeigt keinen Umschalter, wenn die Seite nur eine Sprache hat', async () => {
    await mountView(router({ languages: [pageLangs[0]] }))
    await openNew()
    expect(wrapper.find('[data-test="lang-switch"]').exists()).toBe(false)
  })

  it('zeigt keinen Umschalter, wenn die Sprachen nicht ladbar sind', async () => {
    await mountView(router({ languagesOk: false }))
    await openNew()
    expect(wrapper.find('[data-test="lang-switch"]').exists()).toBe(false)
    expect(wrapper.find('.edit-form').exists()).toBe(true)
  })

  it('zeigt ohne Recht „Ändern“ keinen Umschalter', async () => {
    await mountView(router(), 'incidents:create')
    await openNew()
    expect(wrapper.find('[data-test="lang-switch"]').exists()).toBe(false)
  })

  it('hält den Text der Standardsprache beim Wechseln und zeigt je Sprache eigene Felder', async () => {
    await mountView(router())
    await openNew()
    await fieldByLabel(wrapper, 'Titel').setValue('Deutsch-Titel')
    await tab('English').trigger('click')
    expect(fieldByLabel(wrapper, 'Titel').element.value).toBe('')
    await fieldByLabel(wrapper, 'Titel').setValue('English title')
    await tab('Deutsch').trigger('click')
    expect(fieldByLabel(wrapper, 'Titel').element.value).toBe('Deutsch-Titel')
    expect(tab('English').text()).toBe('🇬🇧 English · leer') // nur Titel: noch nicht ausgefüllt
  })

  it('speichert Vorfall und Übersetzung: erst POST, dann PUT je Sprache mit der neuen ID, Entwurf ohne Haken', async () => {
    const fetchMock = router()
    await mountView(fetchMock)
    await openNew()
    await fillDefault()
    await tab('English').trigger('click')
    await fieldByLabel(wrapper, 'Titel').setValue('API down')
    await fieldByLabel(wrapper, 'Nachricht / Details').setValue('API is not responding')
    expect(tab('English').text()).toBe('🇬🇧 English · Entwurf')
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()

    const puts = calls(fetchMock, 'PUT', /\/translations\//)
    expect(puts).toHaveLength(1) // Französisch bleibt leer und wird nicht gespeichert
    expect(puts[0][0]).toBe('/api/incidents/99/translations/en')
    expect(JSON.parse(puts[0][1].body)).toEqual({ title: 'API down', message: 'API is not responding', status: 'draft' })
    expect(wrapper.find('.alert-success').exists()).toBe(true)
  })

  it('markiert die Übersetzung mit dem Haken als fertig (confirmed)', async () => {
    const fetchMock = router()
    await mountView(fetchMock)
    await openNew()
    await fillDefault()
    await tab('Français').trigger('click')
    await fieldByLabel(wrapper, 'Titel').setValue('API en panne')
    await fieldByLabel(wrapper, 'Nachricht / Details').setValue('Indisponible')
    await wrapper.find('[data-test="lang-done"]').setValue(true)
    expect(tab('Français').text()).toBe('🇫🇷 Français · fertig')
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()

    const put = calls(fetchMock, 'PUT', /\/translations\/fr$/)[0]
    expect(JSON.parse(put[1].body).status).toBe('confirmed')
  })

  it('der Fertig-Haken erscheint nur in weiteren Sprachen, nicht in der Standardsprache', async () => {
    await mountView(router())
    await openNew()
    expect(wrapper.find('[data-test="lang-done"]').exists()).toBe(false)
    await tab('English').trigger('click')
    expect(wrapper.find('[data-test="lang-done"]').exists()).toBe(true)
  })

  it('lädt beim Bearbeiten die vorhandenen Übersetzungen samt Stand', async () => {
    const fetchMock = router({ translations: [{ LanguageCode: 'en', Title: 'API down', Message: 'Not responding', Status: 'confirmed' }] })
    await mountView(fetchMock)
    const editButton = wrapper.findAll('button').find(b => b.attributes('aria-label')?.includes('API down'))
    await editButton.trigger('click')
    await wrapper.findAll('[role="menuitem"]').find(i => i.text().includes('Bearbeiten')).trigger('click')
    await flushPromises()
    expect(tab('English').text()).toBe('🇬🇧 English · fertig')
    await tab('English').trigger('click')
    expect(fieldByLabel(wrapper, 'Titel').element.value).toBe('API down')
  })

  it('speichert nicht, wenn eine Übersetzung nur den Titel hat, und springt zu dieser Sprache', async () => {
    const fetchMock = router()
    await mountView(fetchMock)
    await openNew()
    await fillDefault()
    await tab('English').trigger('click')
    await fieldByLabel(wrapper, 'Titel').setValue('Nur Titel')
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()

    expect(calls(fetchMock, 'POST', /\/incidents$/)).toHaveLength(0)
    expect(wrapper.find('.alert-warning').text()).toContain('English')
    expect(tab('English').attributes('aria-pressed')).toBe('true')
  })

  it('springt bei fehlendem Pflichtfeld zur Standardsprache zurück', async () => {
    await mountView(router())
    await openNew()
    await tab('English').trigger('click')
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()
    expect(tab('Deutsch').attributes('aria-pressed')).toBe('true')
  })

  it('behandelt leere Antworten für Sprachen und Übersetzungen wie „keine“', async () => {
    await mountView(router({ languages: null, catalogBody: null }))
    await openNew()
    expect(wrapper.find('[data-test="lang-switch"]').exists()).toBe(false)
  })

  it('zeigt ohne Katalogeintrag den Sprachcode und behandelt null als keine Übersetzungen', async () => {
    await mountView(router({ catalogBody: [], translations: null }))
    await openNew()
    expect(tab('en').text()).toBe('🇬🇧 en · leer')
  })

  it('speichert nicht, wenn eine Übersetzung nur die Nachricht hat', async () => {
    const fetchMock = router()
    await mountView(fetchMock)
    await openNew()
    await fillDefault()
    await tab('English').trigger('click')
    await fieldByLabel(wrapper, 'Nachricht / Details').setValue('Nur Nachricht')
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()
    expect(calls(fetchMock, 'POST', /\/incidents$/)).toHaveLength(0)
    expect(tab('English').attributes('aria-pressed')).toBe('true')
  })

  it('zeigt den Sprachcode der Standardsprache, wenn die Seite keine als Standard führt', async () => {
    await mountView(router({ languages: [{ LanguageCode: 'en', IsDefault: false }] }))
    await openNew()
    expect(tab('Standard').text()).toBe(' (Standard)'.trim())
  })
})
