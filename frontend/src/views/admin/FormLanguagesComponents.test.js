import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, RouterLinkStub } from '@vue/test-utils'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { setAuthAsPageRole } from '../../test-support/pageRole'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import { fieldByLabel } from '../../test-support/fields.js'
import AdminComponents from './AdminComponents.vue'

// Sprachumschalter im Komponenten-Formular (Name + optionale Beschreibung).

const json = (body, status = 200) => ({ ok: status < 400, status, json: async () => body })
const catalog = [{ Code: 'de', Name: 'Deutsch' }, { Code: 'en', Name: 'English' }]
const pageLangs = [{ LanguageCode: 'de', IsDefault: true }, { LanguageCode: 'en', IsDefault: false }]
const PERMS = 'components:create,components:update'
const rows = [
  { ID: 5, Name: 'API', Description: 'Kern-API', Status: 'operational', Tags: [] },
  { ID: 6, Name: 'Europa', IsGroup: true, Status: 'group', Tags: [], Children: [] },
]

function router({ languages = pageLangs, translations = [], createdBody } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (method === 'GET' && /\/pages\/\d+\/components$/.test(url)) return json(rows)
    if (method === 'GET' && /\/api\/pages\/\d+\/tags$/.test(url)) return json([])
    if (method === 'GET' && /\/api\/tags/.test(url)) return json([])
    if (method === 'GET' && url === '/api/languages') return json(catalog)
    if (method === 'GET' && /\/pages\/\d+\/languages$/.test(url)) return json(languages)
    if (method === 'GET' && /\/components\/\d+\/translations\/all$/.test(url)) return json(translations)
    if (method === 'POST') return json(createdBody ?? { ID: 77 })
    if (method === 'PUT') return json({ ID: 5 })
    throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
  })
}

let wrapper = null
async function mountView(fetchImpl, perms = PERMS) {
  setAuthAsPageRole('jwt-1', { permissions: perms })
  useTenant().setActiveTenant('1')
  global.fetch = fetchImpl
  wrapper = mount(AdminComponents, { global: { plugins: [i18n], stubs: { ...onyxFormStubs, RouterLink: RouterLinkStub } } })
  await flushPromises()
}
const openNew = async (group = false) => {
  const label = group ? 'Neue Gruppe' : 'Neue Komponente'
  await wrapper.findAll('.page-head button').find(b => b.text().includes(label)).trigger('click')
  await flushPromises()
}
const openEdit = async (name) => {
  const row = wrapper.findAll('tbody tr').find(tr => tr.text().includes(name))
  await row.findAll('[role="menuitem"]').find(i => i.text().includes('Bearbeiten')).trigger('click')
  await flushPromises()
}
const tab = (text) => wrapper.findAll('[data-test="lang-switch"] button:not([data-test^="lang-translate"])').find(b => b.text().includes(text))
const calls = (m, method, re) => m.mock.calls.filter(([u, o]) => (o?.method || 'GET') === method && re.test(u))
const submit = async () => { await wrapper.find('.edit-form').trigger('submit'); await flushPromises() }

describe('Sprachumschalter: Komponenten', () => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    localStorage.clear()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    i18n.global.locale.value = 'de'
  })
  afterEach(() => { wrapper?.unmount(); wrapper = null; vi.restoreAllMocks() })

  it('zeigt den Umschalter mit Standardsprache zuerst', async () => {
    await mountView(router())
    await openNew()
    expect(wrapper.findAll('[data-test="lang-switch"] button:not([data-test^="lang-translate"])').map(b => b.text())).toEqual(['🇩🇪 Deutsch (Standard)', '🇬🇧 English · leer'])
  })

  it('zeigt keinen Umschalter bei nur einer Sprache oder ohne Recht „Ändern“', async () => {
    await mountView(router({ languages: [pageLangs[0]] }))
    await openNew()
    expect(wrapper.find('[data-test="lang-switch"]').exists()).toBe(false)
    wrapper.unmount()
    await mountView(router(), 'components:create')
    await openNew()
    expect(wrapper.find('[data-test="lang-switch"]').exists()).toBe(false)
  })

  it('speichert Komponente und Übersetzung: erst POST, dann PUT mit der neuen ID, Haken = confirmed', async () => {
    const fetchMock = router()
    await mountView(fetchMock)
    await openNew()
    await fieldByLabel(wrapper, 'Name').setValue('Datenbank')
    await tab('English').trigger('click')
    await fieldByLabel(wrapper, 'Name').setValue('Database')
    await fieldByLabel(wrapper, 'Beschreibung').setValue('Main DB')
    await wrapper.find('[data-test="lang-done"]').setValue(true)
    await submit()

    expect(JSON.parse(calls(fetchMock, 'POST', /\/components$/)[0][1].body).name).toBe('Datenbank')
    const put = calls(fetchMock, 'PUT', /\/translations\//)
    expect(put).toHaveLength(1)
    expect(put[0][0]).toBe('/api/components/77/translations/en')
    expect(JSON.parse(put[0][1].body)).toEqual({ title: 'Database', description: 'Main DB', status: 'confirmed' })
    expect(wrapper.find('.alert-success').exists()).toBe(true)
  })

  it('genügt der Name: Übersetzung ohne Beschreibung wird als Entwurf mit leerer Beschreibung gespeichert', async () => {
    const fetchMock = router()
    await mountView(fetchMock)
    await openNew()
    await fieldByLabel(wrapper, 'Name').setValue('Datenbank')
    await tab('English').trigger('click')
    await fieldByLabel(wrapper, 'Name').setValue('Database')
    expect(tab('English').text()).toBe('🇬🇧 English · Entwurf')
    await submit()
    const put = calls(fetchMock, 'PUT', /\/translations\/en$/)
    expect(JSON.parse(put[0][1].body)).toEqual({ title: 'Database', description: '', status: 'draft' })
  })

  it('speichert nicht bei Übersetzung nur mit Beschreibung und springt zu dieser Sprache', async () => {
    const fetchMock = router()
    await mountView(fetchMock)
    await openNew()
    await fieldByLabel(wrapper, 'Name').setValue('Datenbank')
    await tab('English').trigger('click')
    await fieldByLabel(wrapper, 'Beschreibung').setValue('Nur Beschreibung')
    await submit()
    expect(calls(fetchMock, 'POST', /./)).toHaveLength(0)
    expect(tab('English').attributes('aria-pressed')).toBe('true')
    expect(wrapper.find('.alert-warning').exists()).toBe(true)
  })

  it('springt bei fehlendem Namen zur Standardsprache zurück und meldet das Pflichtfeld', async () => {
    const fetchMock = router()
    await mountView(fetchMock)
    await openNew()
    await tab('English').trigger('click')
    await submit()
    expect(tab('Deutsch').attributes('aria-pressed')).toBe('true')
    expect(wrapper.text()).toContain('Bitte einen Namen eingeben.')
    expect(calls(fetchMock, 'POST', /./)).toHaveLength(0)
  })

  it('zählt Zeichen im aktiven Reiter und zeigt Längenfehler der Standardsprache nur dort', async () => {
    await mountView(router())
    await openEdit('API')
    await fieldByLabel(wrapper, 'Name').setValue('x'.repeat(65)) // länger als erlaubt (Altbestand)
    expect(wrapper.text()).toContain('höchstens 60 Zeichen')
    await tab('English').trigger('click')
    expect(wrapper.text()).not.toContain('höchstens 60 Zeichen')
    expect(wrapper.text()).toContain('0 / 60 Zeichen')
    await fieldByLabel(wrapper, 'Name').setValue('Hi')
    await flushPromises()
    expect(wrapper.text()).toContain('2 / 60 Zeichen')
    expect(fieldByLabel(wrapper, 'Name').attributes('maxlength')).toBe('60')
    expect(fieldByLabel(wrapper, 'Beschreibung').attributes('maxlength')).toBe('240')
  })

  it('lädt beim Bearbeiten vorhandene Übersetzungen und ändert sie per PUT ohne neuen Eintrag', async () => {
    const fetchMock = router({ translations: [{ LanguageCode: 'en', Title: 'Old', Description: 'Old desc', Status: 'draft' }] })
    await mountView(fetchMock)
    await openEdit('API')
    expect(tab('English').text()).toBe('🇬🇧 English · Entwurf')
    await tab('English').trigger('click')
    await fieldByLabel(wrapper, 'Name').setValue('New')
    await submit()
    expect(calls(fetchMock, 'POST', /./)).toHaveLength(0)
    const put = calls(fetchMock, 'PUT', /\/translations\/en$/)
    expect(put[0][0]).toBe('/api/components/5/translations/en')
    expect(JSON.parse(put[0][1].body)).toEqual({ title: 'New', description: 'Old desc', status: 'draft' })
  })

  it('übersetzt bei Gruppen den Namen (leere Beschreibung)', async () => {
    const fetchMock = router()
    await mountView(fetchMock)
    await openEdit('Europa')
    await tab('English').trigger('click')
    await fieldByLabel(wrapper, 'Name').setValue('Europe')
    await submit()
    const put = calls(fetchMock, 'PUT', /\/components\/6\/translations\/en$/)
    expect(JSON.parse(put[0][1].body)).toEqual({ title: 'Europe', description: '', status: 'draft' })
  })

  it('legt die Komponente auch an, wenn die Antwort keine ID enthält und keine Übersetzung ausgefüllt ist', async () => {
    const fetchMock = router({ createdBody: {} })
    await mountView(fetchMock)
    await openNew()
    await fieldByLabel(wrapper, 'Name').setValue('Datenbank')
    await submit()
    expect(calls(fetchMock, 'POST', /./)).toHaveLength(1)
    expect(calls(fetchMock, 'PUT', /\/translations\//)).toHaveLength(0)
    expect(wrapper.find('.alert-success').exists()).toBe(true)
  })

  it('speichert bei nur einer Seitensprache ohne Übersetzungsaufrufe', async () => {
    const fetchMock = router({ languages: [pageLangs[0]] })
    await mountView(fetchMock)
    await openNew()
    await fieldByLabel(wrapper, 'Name').setValue('Datenbank')
    await submit()
    expect(calls(fetchMock, 'POST', /./)).toHaveLength(1)
    expect(calls(fetchMock, 'PUT', /\/translations\//)).toHaveLength(0)
  })
})
