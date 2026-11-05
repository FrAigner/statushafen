import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { setAuthAsPageRole } from '../../test-support/pageRole'
import { useTenant } from '../../composables/useTenant'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import { componentTreeFixture } from '../../test-support/componentTree'
import AdminMaintenance from './AdminMaintenance.vue'
import AdminNotices from './AdminNotices.vue'
import { fieldByLabel } from '../../test-support/fieldByLabel.js'

// Sprachumschalter in Wartung und Ankündigungen (Vorfälle: AdminIncidentsLanguages.test.js).

const json = (body, status = 200) => ({ ok: status < 400, status, json: async () => body })
const catalog = [{ Code: 'de', Name: 'Deutsch' }, { Code: 'en', Name: 'English' }]
const pageLangs = [{ LanguageCode: 'de', IsDefault: true }, { LanguageCode: 'en', IsDefault: false }]

const VIEWS = [
  {
    name: 'Wartung', component: AdminMaintenance, perms: 'maintenance:create,maintenance:update',
    listRe: /\/pages\/\d+\/incidents\?is_maintenance=true&limit=200$/, createdId: 77, saveBase: '/api/incidents/77/translations/', transRe: /\/incidents\/\d+\/translations\/all$/,
    row: { ID: 5, Title: 'DB-Upgrade', Message: 'Kurz offline', Status: 'scheduled', IsMaintenance: true, Components: [{ ID: 10, Name: 'API' }], CreatedAt: '2026-07-01T10:00:00Z', ScheduledStartAt: '2026-10-02T08:00:00Z', ScheduledEndAt: '2026-10-02T09:00:00Z' },
    fill: async (w) => {
      await fieldByLabel(w, 'Titel').setValue('Wartung')
      await fieldByLabel(w, 'Nachricht / Details').setValue('Details')
      await w.find('.onyx-select-stub-option input[type="checkbox"]').setValue(true)
      await fieldByLabel(w, 'Start').setValue('2026-10-02T08:00')
      await fieldByLabel(w, 'Ende').setValue('2026-10-02T09:00')
    },
  },
  {
    name: 'Ankündigungen', component: AdminNotices, perms: 'notices:create,notices:update',
    listRe: /\/pages\/\d+\/notices\/all$/, createdId: 88, saveBase: '/api/notices/88/translations/', transRe: /\/notices\/\d+\/translations\/all$/,
    row: { ID: 5, Title: 'Hinweis', Message: 'Text', Level: 'info', IsActive: true, CreatedAt: '2026-07-01T10:00:00Z' },
    fill: async (w) => {
      await fieldByLabel(w, 'Titel').setValue('Hinweis')
      await fieldByLabel(w, 'Nachricht / Details').setValue('Text')
    },
  },
]

function router(v, { languages = pageLangs, translations = [], createdBody } = {}) {
  return vi.fn(async (url, options = {}) => {
    const method = options.method || 'GET'
    if (method === 'GET' && /\/incident-templates$/.test(url)) return json([])
    if (method === 'GET' && v.listRe.test(url)) return json([v.row])
    if (method === 'GET' && /\/components$/.test(url)) return json(componentTreeFixture())
    if (method === 'GET' && /\/api\/pages\/\d+\/tags$/.test(url)) return json([])
    if (method === 'GET' && /\/api\/tags/.test(url)) return json([])
    if (method === 'GET' && url === '/api/languages') return json(catalog)
    if (method === 'GET' && /\/pages\/\d+\/languages$/.test(url)) return json(languages)
    if (method === 'GET' && v.transRe.test(url)) return json(translations)
    if (method === 'POST' && !/translations/.test(url)) return json(createdBody ?? { ID: v.createdId })
    if (method === 'PUT') return json({ ID: v.row.ID })
    throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
  })
}

let wrapper = null
async function mountView(v, fetchImpl, perms = v.perms) {
  setAuthAsPageRole('jwt-1', { permissions: perms })
  const r = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { template: '<div/>' } }] })
  r.push('/')
  await r.isReady()
  useTenant().setActiveTenant('1')
  global.fetch = fetchImpl
  wrapper = mount(v.component, { global: { plugins: [i18n, r], stubs: onyxFormStubs } })
  await flushPromises()
  return wrapper
}
const openNew = async () => { await wrapper.find('.page-head button').trigger('click'); await flushPromises() }
const tab = (text) => wrapper.findAll('[data-test="lang-switch"] button:not([data-test^="lang-translate"])').find(b => b.text().includes(text))
const calls = (m, method, re) => m.mock.calls.filter(([u, o]) => (o?.method || 'GET') === method && re.test(u))

describe.each(VIEWS)('Sprachumschalter: $name', (v) => {
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
    await mountView(v, router(v))
    await openNew()
    expect(wrapper.findAll('[data-test="lang-switch"] button:not([data-test^="lang-translate"])').map(b => b.text())).toEqual(['🇩🇪 Deutsch (Standard)', '🇬🇧 English · leer'])
  })

  it('zeigt keinen Umschalter bei nur einer Sprache oder ohne Recht „Ändern“', async () => {
    await mountView(v, router(v, { languages: [pageLangs[0]] }))
    await openNew()
    expect(wrapper.find('[data-test="lang-switch"]').exists()).toBe(false)
    wrapper.unmount()
    await mountView(v, router(v), v.perms.split(',')[0])
    await openNew()
    expect(wrapper.find('[data-test="lang-switch"]').exists()).toBe(false)
  })

  it('speichert Eintrag und Übersetzung: erst POST, dann PUT mit der neuen ID, Haken = confirmed', async () => {
    const fetchMock = router(v)
    await mountView(v, fetchMock)
    await openNew()
    await v.fill(wrapper)
    await tab('English').trigger('click')
    await fieldByLabel(wrapper, 'Titel').setValue('Title')
    await fieldByLabel(wrapper, 'Nachricht / Details').setValue('Body')
    await wrapper.find('[data-test="lang-done"]').setValue(true)
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()

    const put = calls(fetchMock, 'PUT', /\/translations\//)
    expect(put).toHaveLength(1)
    expect(put[0][0]).toBe(`${v.saveBase}en`)
    expect(JSON.parse(put[0][1].body)).toEqual({ title: 'Title', message: 'Body', status: 'confirmed' })
    expect(wrapper.find('.alert-success').exists()).toBe(true)
  })

  it('lädt beim Bearbeiten vorhandene Übersetzungen', async () => {
    const fetchMock = router(v, { translations: [{ LanguageCode: 'en', Title: 'Title', Message: 'Body', Status: 'draft' }] })
    await mountView(v, fetchMock)
    const menu = wrapper.findAll('button').find(b => b.attributes('aria-label')?.includes(v.row.Title))
    await menu.trigger('click')
    await wrapper.findAll('[role="menuitem"]').find(i => i.text().includes('Bearbeiten')).trigger('click')
    await flushPromises()
    expect(tab('English').text()).toBe('🇬🇧 English · Entwurf')
  })

  it('speichert nicht bei halber Übersetzung und springt zu dieser Sprache', async () => {
    const fetchMock = router(v)
    await mountView(v, fetchMock)
    await openNew()
    await v.fill(wrapper)
    await tab('English').trigger('click')
    await fieldByLabel(wrapper, 'Titel').setValue('Nur Titel')
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()
    expect(calls(fetchMock, 'POST', /./)).toHaveLength(0)
    expect(tab('English').attributes('aria-pressed')).toBe('true')
    expect(wrapper.find('.alert-warning').exists()).toBe(true)
  })

  it('springt bei fehlendem Pflichtfeld zur Standardsprache zurück', async () => {
    await mountView(v, router(v))
    await openNew()
    await tab('English').trigger('click')
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()
    expect(tab('Deutsch').attributes('aria-pressed')).toBe('true')
  })

  it('speichert beim Bearbeiten die geänderte Übersetzung per PUT, ohne neuen Eintrag anzulegen', async () => {
    const fetchMock = router(v, { translations: [{ LanguageCode: 'en', Title: 'Old', Message: 'Old body', Status: 'draft' }] })
    await mountView(v, fetchMock)
    const menu = wrapper.findAll('button').find(b => b.attributes('aria-label')?.includes(v.row.Title))
    await menu.trigger('click')
    await wrapper.findAll('[role="menuitem"]').find(i => i.text().includes('Bearbeiten')).trigger('click')
    await flushPromises()
    await tab('English').trigger('click')
    await fieldByLabel(wrapper, 'Titel').setValue('New')
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()

    expect(calls(fetchMock, 'POST', /./)).toHaveLength(0)
    const put = calls(fetchMock, 'PUT', /\/translations\/en$/)
    expect(put[0][0]).toBe(`/api/${v.name === 'Ankündigungen' ? 'notices' : 'incidents'}/5/translations/en`)
    expect(JSON.parse(put[0][1].body)).toMatchObject({ title: 'New', message: 'Old body', status: 'draft' })
  })

  it('legt den Eintrag auch an, wenn die Antwort keine ID enthält und keine Übersetzung ausgefüllt ist', async () => {
    const fetchMock = router(v, { createdBody: {} })
    await mountView(v, fetchMock)
    await openNew()
    await v.fill(wrapper)
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()
    expect(calls(fetchMock, 'POST', /./)).toHaveLength(1)
    expect(calls(fetchMock, 'PUT', /\/translations\//)).toHaveLength(0)
    expect(wrapper.find('.alert-success').exists()).toBe(true)
  })

  it('speichert bei nur einer Seitensprache ohne Umschalter und ohne Übersetzungsaufrufe', async () => {
    const fetchMock = router(v, { languages: [pageLangs[0]] })
    await mountView(v, fetchMock)
    await openNew()
    await v.fill(wrapper)
    await wrapper.find('.edit-form').trigger('submit')
    await flushPromises()
    expect(calls(fetchMock, 'POST', /./)).toHaveLength(1)
    expect(calls(fetchMock, 'PUT', /\/translations\//)).toHaveLength(0)
    expect(wrapper.find('.alert-success').exists()).toBe(true)
  })
})
