import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { setAuthAsPageRole } from '../../test-support/pageRole'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import { componentTreeFixture } from '../../test-support/componentTree'
import { fieldByLabel } from '../../test-support/fields.js'
import AdminIncidentTemplates from './AdminIncidentTemplates.vue'
import AdminMaintenance from './AdminMaintenance.vue'
import AdminIncidents from './AdminIncidents.vue'

// Sprachumschalter im Vorlagen-Formular und „Vorlage verwenden“ mit den Übersetzungen der Vorlage.

const json = (body, status = 200) => ({ ok: status < 400, status, json: async () => body })
const catalog = [{ Code: 'de', Name: 'Deutsch' }, { Code: 'en', Name: 'English' }]
const pageLangs = [{ LanguageCode: 'de', IsDefault: true }, { LanguageCode: 'en', IsDefault: false }]
const TPL_PERMS = 'incident_templates:create,incident_templates:update'
const template = { ID: 3, Name: 'DB-Ausfall', Kind: 'incident', Title: 'Datenbank down', Message: 'Wir prüfen', DefaultStatus: 'investigating', ComponentIDs: '10' }
const tplTranslations = [{ LanguageCode: 'en', Title: 'Database down', Message: 'We are looking into it', Status: 'confirmed' }]

function router({ languages = pageLangs, translations = [], translationsOk = true, tpl = template, translationsBody } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (method === 'GET' && /\/incident-templates$/.test(url)) return json([tpl])
    if (method === 'GET' && /\/incident-templates\/\d+\/translations$/.test(url)) return translationsOk ? (translationsBody ? translationsBody() : json(translations)) : json({ error: 'x' }, 500)
    if (method === 'GET' && /\/components$/.test(url)) return json(componentTreeFixture())
    if (method === 'GET' && /\/pages\/\d+\/incidents/.test(url)) return json([])
    if (method === 'GET' && /\/api\/pages\/\d+\/tags$/.test(url)) return json([])
    if (method === 'GET' && /\/api\/tags/.test(url)) return json([])
    if (method === 'GET' && url === '/api/languages') return json(catalog)
    if (method === 'GET' && /\/pages\/\d+\/languages$/.test(url)) return json(languages)
    if (method === 'POST') return json({ ID: 77 })
    if (method === 'PUT') return json({ ID: 3 })
    throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
  })
}

let wrapper = null
async function mountView(component, fetchImpl, perms) {
  setAuthAsPageRole('jwt-1', { permissions: perms })
  const r = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { template: '<div/>' } }] })
  r.push('/')
  await r.isReady()
  useTenant().setActiveTenant('1')
  global.fetch = fetchImpl
  wrapper = mount(component, { global: { plugins: [i18n, r], stubs: onyxFormStubs } })
  await flushPromises()
}
const openNew = async () => { await wrapper.find('.page-head button').trigger('click'); await flushPromises() }
const tab = (text) => wrapper.findAll('[data-test="lang-switch"] button:not([data-test^="lang-translate"])').find(b => b.text().includes(text))
const calls = (m, method, re) => m.mock.calls.filter(([u, o]) => (o?.method || 'GET') === method && re.test(u))
const submit = async () => { await wrapper.find('.edit-form').trigger('submit'); await flushPromises() }

beforeEach(() => {
  useAuth().logout()
  useTenant().setActiveTenant(null)
  useTenant().setMyPages([])
  localStorage.clear()
  vi.spyOn(console, 'error').mockImplementation(() => {})
  i18n.global.locale.value = 'de'
})
afterEach(() => { wrapper?.unmount(); wrapper = null; vi.restoreAllMocks() })

describe('Sprachumschalter: Vorlagen', () => {
  it('zeigt den Umschalter mit Standardsprache zuerst, bei nur einer Sprache oder ohne Recht „Ändern“ nicht', async () => {
    await mountView(AdminIncidentTemplates, router(), TPL_PERMS)
    await openNew()
    expect(wrapper.findAll('[data-test="lang-switch"] button:not([data-test^="lang-translate"])').map(b => b.text())).toEqual(['🇩🇪 Deutsch (Standard)', '🇬🇧 English · leer'])
    wrapper.unmount()
    await mountView(AdminIncidentTemplates, router({ languages: [pageLangs[0]] }), TPL_PERMS)
    await openNew()
    expect(wrapper.find('[data-test="lang-switch"]').exists()).toBe(false)
    wrapper.unmount()
    await mountView(AdminIncidentTemplates, router(), 'incident_templates:create')
    await openNew()
    expect(wrapper.find('[data-test="lang-switch"]').exists()).toBe(false)
  })

  it('speichert Vorlage und Übersetzung: erst POST, dann PUT mit der neuen ID, Haken = confirmed', async () => {
    const fetchMock = router()
    await mountView(AdminIncidentTemplates, fetchMock, TPL_PERMS)
    await openNew()
    await fieldByLabel(wrapper, 'Interner Vorlagenname').setValue('Neu')
    await fieldByLabel(wrapper, 'Titel').setValue('Titel de')
    await fieldByLabel(wrapper, 'Nachricht / Details').setValue('Text de')
    await tab('English').trigger('click')
    await fieldByLabel(wrapper, 'Titel').setValue('Title en')
    await fieldByLabel(wrapper, 'Nachricht / Details').setValue('Text en')
    await wrapper.find('[data-test="lang-done"]').setValue(true)
    await submit()

    expect(calls(fetchMock, 'POST', /incident-templates$/)).toHaveLength(1)
    const put = calls(fetchMock, 'PUT', /\/translations\//)
    expect(put).toHaveLength(1)
    expect(put[0][0]).toBe('/api/pages/1/incident-templates/77/translations/en')
    expect(JSON.parse(put[0][1].body)).toEqual({ title: 'Title en', message: 'Text en', status: 'confirmed' })
    expect(wrapper.find('.alert-success').exists()).toBe(true)
  })

  it('lädt beim Bearbeiten vorhandene Übersetzungen und speichert Änderungen per PUT', async () => {
    const fetchMock = router({ translations: tplTranslations })
    await mountView(AdminIncidentTemplates, fetchMock, TPL_PERMS)
    const menu = wrapper.findAll('button').find(b => b.attributes('aria-label')?.includes('DB-Ausfall'))
    await menu.trigger('click')
    await wrapper.findAll('[role="menuitem"]').find(i => i.text().includes('Bearbeiten')).trigger('click')
    await flushPromises()
    expect(tab('English').text()).toBe('🇬🇧 English · fertig')
    await tab('English').trigger('click')
    await fieldByLabel(wrapper, 'Titel').setValue('Changed')
    await submit()
    expect(calls(fetchMock, 'POST', /./)).toHaveLength(0)
    const put = calls(fetchMock, 'PUT', /\/translations\/en$/)
    expect(put[0][0]).toBe('/api/pages/1/incident-templates/3/translations/en')
    expect(JSON.parse(put[0][1].body)).toMatchObject({ title: 'Changed', message: 'We are looking into it', status: 'confirmed' })
  })

  it('speichert nicht bei halber Übersetzung und springt zu dieser Sprache', async () => {
    const fetchMock = router()
    await mountView(AdminIncidentTemplates, fetchMock, TPL_PERMS)
    await openNew()
    await fieldByLabel(wrapper, 'Interner Vorlagenname').setValue('Neu')
    await fieldByLabel(wrapper, 'Titel').setValue('T')
    await fieldByLabel(wrapper, 'Nachricht / Details').setValue('M')
    await tab('English').trigger('click')
    await fieldByLabel(wrapper, 'Titel').setValue('Nur Titel')
    await submit()
    expect(calls(fetchMock, 'POST', /./)).toHaveLength(0)
    expect(tab('English').attributes('aria-pressed')).toBe('true')
    expect(wrapper.find('.alert-warning').exists()).toBe(true)
  })
})

const USE_VIEWS = [
  { name: 'Vorfälle', component: AdminIncidents, perms: 'incidents:create,incident_templates:read,incidents:update', saveRe: /\/translations\/en$/, tpl: template },
  { name: 'Wartung', component: AdminMaintenance, perms: 'maintenance:create,incident_templates:read,maintenance:update', saveRe: /\/translations\/en$/, tpl: { ...template, Kind: 'maintenance' } },
]

describe.each(USE_VIEWS)('Vorlage verwenden: $name', (v) => {
  const choose = async () => {
    const select = wrapper.findAll('select').find(s => s.text().includes('DB-Ausfall'))
    await select.setValue('3')
    await flushPromises()
  }

  it('übernimmt die Übersetzungen der Vorlage in die Sprachen des Formulars', async () => {
    const fetchMock = router({ translations: tplTranslations, tpl: v.tpl })
    await mountView(v.component, fetchMock, v.perms)
    await openNew()
    expect(tab('English').text()).toContain('leer')
    await choose()
    expect(calls(fetchMock, 'GET', /incident-templates\/3\/translations$/)).toHaveLength(1)
    expect(tab('English').text()).toBe('🇬🇧 English · fertig')
    await tab('English').trigger('click')
    expect(fieldByLabel(wrapper, 'Titel').element.value).toBe('Database down')
    expect(fieldByLabel(wrapper, 'Nachricht / Details').element.value).toBe('We are looking into it')
  })

  it('fragt ohne Sprachumschalter keine Übersetzungen der Vorlage ab', async () => {
    const fetchMock = router({ languages: [pageLangs[0]], translations: tplTranslations, tpl: v.tpl })
    await mountView(v.component, fetchMock, v.perms)
    await openNew()
    await choose()
    expect(calls(fetchMock, 'GET', /incident-templates\/3\/translations$/)).toHaveLength(0)
    expect(fieldByLabel(wrapper, 'Titel').element.value).toBe('Datenbank down')
  })

  it('übernimmt trotzdem die Standardsprache, wenn die Übersetzungen der Vorlage nicht ladbar sind', async () => {
    await mountView(v.component, router({ translationsOk: false, tpl: v.tpl }), v.perms)
    await openNew()
    await choose()
    expect(fieldByLabel(wrapper, 'Titel').element.value).toBe('Datenbank down')
    expect(tab('English').text()).toContain('leer')
  })

  it('übernimmt nichts, wenn die Vorlage gewechselt wurde, bevor die Übersetzungen ankamen', async () => {
    let release
    const gate = new Promise(resolve => { release = resolve })
    const fetchMock = router({ tpl: v.tpl, translationsBody: async () => { await gate; return json(tplTranslations) } })
    await mountView(v.component, fetchMock, v.perms)
    await openNew()
    const select = wrapper.findAll('select').find(s => s.text().includes('DB-Ausfall'))
    await select.setValue('3')
    await select.setValue('')
    release()
    await flushPromises()
    expect(tab('English').text()).toContain('leer')
  })

  it('kommt mit einer leeren Antwort (null) der Vorlage zurecht', async () => {
    await mountView(v.component, router({ tpl: v.tpl, translationsBody: async () => json(null) }), v.perms)
    await openNew()
    await choose()
    expect(fieldByLabel(wrapper, 'Titel').element.value).toBe('Datenbank down')
    expect(tab('English').text()).toContain('leer')
  })
})

describe('Vorlagen ansehen ohne Recht „Ändern“', () => {
  it('öffnet schreibgeschützt ohne Umschalter und ohne Abruf der Übersetzungen', async () => {
    const fetchMock = router({ translations: tplTranslations })
    await mountView(AdminIncidentTemplates, fetchMock, 'incident_templates:create')
    const menu = wrapper.findAll('button').find(b => b.attributes('aria-label')?.includes('DB-Ausfall'))
    await menu.trigger('click')
    await wrapper.findAll('[role="menuitem"]').find(i => i.text().includes('Ansehen')).trigger('click')
    await flushPromises()
    expect(wrapper.find('.edit-form').exists()).toBe(true)
    expect(wrapper.find('[data-test="lang-switch"]').exists()).toBe(false)
    expect(calls(fetchMock, 'GET', /\/translations$/)).toHaveLength(0)
  })
})
