import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../i18n'
import { useAuth } from '../../composables/useAuth'
import { useTenant } from '../../composables/useTenant'
import { setAuthAsPageRole } from '../../test-support/pageRole'
import { onyxFormStubs } from '../../test-support/onyxStubs'
import { fieldByLabel } from '../../test-support/fieldByLabel.js'
import AdminIncidentDetail from './AdminIncidentDetail.vue'

// Sprachumschalter im Formular „Update posten“ und Editor „Übersetzungen“
// an einem Zeitleisten-Eintrag. Ein Update hat nur eine Nachricht (kein Titel).

const json = (body, status = 200) => ({ ok: status < 400, status, json: async () => body })
const catalog = [{ Code: 'de', Name: 'Deutsch' }, { Code: 'en', Name: 'English' }, { Code: 'fr', Name: 'Français' }]
const pageLangs = [
  { LanguageCode: 'de', IsDefault: true },
  { LanguageCode: 'en', IsDefault: false },
  { LanguageCode: 'fr', IsDefault: false },
]
const incident = (extra = {}) => ({
  ID: 1, Title: 'API down', Status: 'investigating', CreatedAt: '2026-07-01T10:00:00Z', Components: [], Tags: [],
  updates: [{ id: 7, status: 'investigating', message: 'Untersuchung läuft', created_at: '2026-07-01T10:05:00Z' }],
  ...extra,
})

function router({ languages = pageLangs, inc = incident(), translations = [], postFails = false, putFails = false, translateBody = { title: '', message: 'Under investigation', provider: 'libretranslate' } } = {}) {
  return vi.fn(async (url, o = {}) => {
    const method = o.method || 'GET'
    if (url === '/api/languages') return json(catalog)
    if (/\/api\/pages\/\d+\/languages$/.test(url)) return json(languages)
    if (method === 'GET' && /\/incidents\/1$/.test(url)) return json(inc)
    if (method === 'POST' && /\/incidents\/1\/updates$/.test(url)) return postFails ? json({ error: 'x' }, 500) : json({ id: 8 })
    if (method === 'GET' && /\/updates\/7\/translations$/.test(url)) return json(translations)
    if (method === 'PUT' && /\/updates\/\d+\/translations\/[a-z]+$/.test(url)) return putFails ? json({ error: 'kaputt' }, 500) : json({})
    if (method === 'POST' && /\/translate$/.test(url)) return json(translateBody)
    throw new Error(`Unerwarteter Fetch-Aufruf: ${method} ${url}`)
  })
}

let wrapper = null
async function mountView(fetchImpl, perms = 'incidents:create,incidents:update') {
  setAuthAsPageRole('jwt-1', { permissions: perms })
  const r = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/incidents/:pageId/:id', component: AdminIncidentDetail }] })
  r.push('/admin/incidents/1/1')
  await r.isReady()
  global.fetch = fetchImpl
  wrapper = mount(AdminIncidentDetail, { global: { plugins: [i18n, r], stubs: onyxFormStubs } })
  await flushPromises()
  return wrapper
}
const form = () => wrapper.find('.update-form')
const tab = (root, text) => root.findAll('[data-test="lang-switch"] button:not([data-test^="lang-translate"])').find(b => b.text().includes(text))
const calls = (fetchMock, method, re) => fetchMock.mock.calls.filter(([u, o]) => (o?.method || 'GET') === method && re.test(u))
const openEditor = async () => {
  await wrapper.find('[data-test="update-translations-open"]').trigger('click')
  await flushPromises()
  return wrapper.find('[data-test="update-translations"]')
}

describe('AdminIncidentDetail: Update-Übersetzungen', () => {
  beforeEach(() => {
    useAuth().logout()
    useTenant().setActiveTenant(null)
    useTenant().setMyPages([])
    localStorage.clear()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    i18n.global.locale.value = 'de'
  })
  afterEach(() => { wrapper?.unmount(); wrapper = null; vi.restoreAllMocks() })

  it('zeigt Umschalter und „Übersetzungen“-Knopf nur bei mehreren Sprachen und mit Recht „Ändern“', async () => {
    await mountView(router())
    expect(form().find('[data-test="lang-switch"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="update-translations-open"]').exists()).toBe(true)
    wrapper.unmount()

    await mountView(router({ languages: [pageLangs[0]] }))
    expect(wrapper.find('[data-test="lang-switch"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="update-translations-open"]').exists()).toBe(false)
    wrapper.unmount()

    await mountView(router(), 'incidents:create') // darf posten, aber nicht übersetzen
    expect(form().exists()).toBe(true)
    expect(wrapper.find('[data-test="lang-switch"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="update-translations-open"]').exists()).toBe(false)
  })

  it('verlangt bei einer Wartung das Recht maintenance:update', async () => {
    await mountView(router({ inc: incident({ IsMaintenance: true }) }), 'incidents:create,incidents:update')
    expect(wrapper.find('[data-test="lang-switch"]').exists()).toBe(false)
    wrapper.unmount()
    await mountView(router({ inc: incident({ IsMaintenance: true }) }), 'incidents:create,maintenance:update')
    expect(wrapper.find('[data-test="lang-switch"]').exists()).toBe(true)
  })

  it('postet das Update und speichert danach die ausgefüllten Sprachen ohne Titel', async () => {
    const fetchMock = router()
    await mountView(fetchMock)
    await fieldByLabel(wrapper, 'Update-Nachricht').setValue('Ursache gefunden')
    await tab(form(), 'English').trigger('click')
    await fieldByLabel(wrapper, 'Update-Nachricht').setValue('Cause found')
    expect(tab(form(), 'English').text()).toBe('🇬🇧 English · Entwurf')
    await wrapper.find('[data-test="lang-done"]').setValue(true)
    expect(tab(form(), 'English').text()).toBe('🇬🇧 English · fertig')
    await form().find('form').trigger('submit')
    await flushPromises()

    const post = calls(fetchMock, 'POST', /\/updates$/)
    expect(JSON.parse(post[0][1].body)).toEqual({ status: 'investigating', message: 'Ursache gefunden' })
    const puts = calls(fetchMock, 'PUT', /\/translations\//)
    expect(puts).toHaveLength(1) // Französisch bleibt leer
    expect(puts[0][0]).toBe('/api/pages/1/incidents/1/updates/8/translations/en')
    expect(JSON.parse(puts[0][1].body)).toEqual({ message: 'Cause found', status: 'confirmed' })
    expect(form().find('.alert-error').exists()).toBe(false)
    // Formular ist nach dem Neuladen zurückgesetzt (Standardsprache, leere Nachricht).
    expect(tab(form(), 'English').text()).toBe('🇬🇧 English · leer')
    expect(fieldByLabel(wrapper, 'Update-Nachricht').element.value).toBe('')
  })

  it('sendet ohne ausgefüllte Übersetzung keinen Übersetzungsaufruf', async () => {
    const fetchMock = router()
    await mountView(fetchMock)
    await fieldByLabel(wrapper, 'Update-Nachricht').setValue('Nur Deutsch')
    await form().find('form').trigger('submit')
    await flushPromises()
    expect(calls(fetchMock, 'PUT', /\/translations\//)).toHaveLength(0)
  })

  it('behält das Update, wenn nur die Übersetzung scheitert, und nennt den Weg zum Nachholen', async () => {
    const fetchMock = router({ putFails: true })
    await mountView(fetchMock)
    await fieldByLabel(wrapper, 'Update-Nachricht').setValue('Text')
    await tab(form(), 'English').trigger('click')
    await fieldByLabel(wrapper, 'Update-Nachricht').setValue('Text EN')
    await form().find('form').trigger('submit')
    await flushPromises()
    expect(calls(fetchMock, 'POST', /\/updates$/)).toHaveLength(1)
    expect(form().find('.alert-error').text()).toContain('Das Update wurde gepostet')
    expect(form().find('.alert-error').text()).toContain('kaputt')
  })

  it('speichert keine Übersetzung, wenn das Posten scheitert', async () => {
    const fetchMock = router({ postFails: true })
    await mountView(fetchMock)
    await fieldByLabel(wrapper, 'Update-Nachricht').setValue('Text')
    await tab(form(), 'English').trigger('click')
    await fieldByLabel(wrapper, 'Update-Nachricht').setValue('Text EN')
    await form().find('form').trigger('submit')
    await flushPromises()
    expect(calls(fetchMock, 'PUT', /\/translations\//)).toHaveLength(0)
    expect(form().find('.alert-error').text()).toContain('Update fehlgeschlagen')
  })

  it('übersetzt die Nachricht per Knopf ohne Titel und trägt sie als Entwurf ein', async () => {
    const fetchMock = router()
    await mountView(fetchMock)
    await tab(form(), 'English').trigger('click')
    const btn = () => wrapper.find('[data-test="lang-translate"]')
    expect(btn().attributes('disabled')).toBeDefined() // ohne Nachricht in der Standardsprache
    expect(wrapper.text()).toContain('zuerst die Nachricht in der Standardsprache')
    await tab(form(), 'Deutsch').trigger('click')
    await fieldByLabel(wrapper, 'Update-Nachricht').setValue('Wir untersuchen')
    await tab(form(), 'English').trigger('click')
    await btn().trigger('click')
    await flushPromises()

    const [, o] = calls(fetchMock, 'POST', /\/translate$/)[0]
    expect(JSON.parse(o.body)).toEqual({ target_language: 'en', message: 'Wir untersuchen' })
    expect(fieldByLabel(wrapper, 'Update-Nachricht').element.value).toBe('Under investigation')
    expect(tab(form(), 'English').text()).toBe('🇬🇧 English · Entwurf')
  })

  it('öffnet den Editor am Eintrag, lädt vorhandene Übersetzungen und zeigt den Originaltext schreibgeschützt', async () => {
    const fetchMock = router({ translations: [{ LanguageCode: 'en', Message: 'Investigating', Status: 'confirmed' }] })
    await mountView(fetchMock)
    const editor = await openEditor()
    expect(editor.exists()).toBe(true)
    expect(calls(fetchMock, 'GET', /\/updates\/7\/translations$/)[0][0]).toBe('/api/pages/1/incidents/1/updates/7/translations')
    expect(wrapper.find('[data-test="update-translations-open"]').exists()).toBe(false)

    const area = () => editor.find('textarea')
    expect(area().element.value).toBe('Untersuchung läuft')
    expect(area().attributes('readonly')).toBeDefined()
    await tab(editor, 'English').trigger('click')
    expect(area().element.value).toBe('Investigating')
    expect(area().attributes('readonly')).toBeUndefined()
    expect(tab(editor, 'English').text()).toBe('🇬🇧 English · fertig')
  })

  it('speichert im Editor alle ausgefüllten Sprachen und schließt ihn', async () => {
    const fetchMock = router({ translations: [{ LanguageCode: 'en', Message: 'Investigating', Status: 'confirmed' }] })
    await mountView(fetchMock)
    const editor = await openEditor()
    await tab(editor, 'English').trigger('click')
    await editor.find('textarea').setValue('Still investigating')
    await tab(editor, 'Français').trigger('click')
    await editor.find('textarea').setValue('Enquête en cours')
    await wrapper.find('[data-test="update-translations-save"]').trigger('click')
    await flushPromises()

    const puts = calls(fetchMock, 'PUT', /\/translations\//)
    expect(puts.map(([u]) => u)).toEqual([
      '/api/pages/1/incidents/1/updates/7/translations/en',
      '/api/pages/1/incidents/1/updates/7/translations/fr',
    ])
    expect(JSON.parse(puts[0][1].body)).toEqual({ message: 'Still investigating', status: 'confirmed' })
    expect(JSON.parse(puts[1][1].body)).toEqual({ message: 'Enquête en cours', status: 'draft' })
    expect(wrapper.find('[data-test="update-translations"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="update-translations-open"]').exists()).toBe(true)
  })

  it('zeigt einen Fehler im Editor, wenn das Speichern scheitert, und bleibt offen; Abbrechen schließt', async () => {
    const fetchMock = router({ putFails: true })
    await mountView(fetchMock)
    const editor = await openEditor()
    await tab(editor, 'English').trigger('click')
    await editor.find('textarea').setValue('Text')
    await wrapper.find('[data-test="update-translations-save"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="update-translations"] .alert-error').text()).toContain('Übersetzungen konnten nicht gespeichert werden')
    expect(wrapper.find('[data-test="update-translations"]').exists()).toBe(true)

    const cancel = wrapper.findAll('[data-test="update-translations"] button').find(b => b.text() === 'Abbrechen')
    await cancel.trigger('click')
    expect(wrapper.find('[data-test="update-translations"]').exists()).toBe(false)
  })
})
