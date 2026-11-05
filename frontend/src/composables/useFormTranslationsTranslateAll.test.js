import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import i18n from '../i18n'
import { onyxFormStubs } from '../test-support/onyxStubs'
import { useApi } from './useApi'
import { useFormTranslations } from './useFormTranslations'
import FormLanguageSwitch from '../components/FormLanguageSwitch.vue'

// „Alles übersetzen“: alle noch leeren weiteren Sprachen nacheinander als Entwurf.
// Nur „Sprache nicht unterstützt“ überspringt; jeder andere Fehler bricht den Lauf ab (Review).

const json = (body, status = 200) => ({ ok: status < 400, status, json: async () => body })
const catalog = [{ Code: 'de', Name: 'Deutsch' }, { Code: 'en', Name: 'English' }, { Code: 'fr', Name: 'Français' }, { Code: 'it', Name: 'Italiano' }]
const pageLangs = [
  { LanguageCode: 'de', IsDefault: true }, { LanguageCode: 'en', IsDefault: false },
  { LanguageCode: 'fr', IsDefault: false }, { LanguageCode: 'it', IsDefault: false },
]

let wrapper
let trans
let formData
let fetchMock

// translateHandler(target, body) -> Antwort oder wirft
async function setup(translateHandler, { titleRequired = true, data } = {}) {
  fetchMock = vi.fn(async (url, o = {}) => {
    if (url === '/api/languages') return json(catalog)
    if (/\/languages$/.test(url)) return json(pageLangs)
    if (/\/translate$/.test(url)) {
      const body = JSON.parse(o.body)
      return translateHandler(body.target_language, body)
    }
    throw new Error(`unerwartet ${url}`)
  })
  global.fetch = fetchMock
  formData = ref(data ?? (titleRequired ? { Title: 'Störung', Message: 'Details' } : { Message: 'Details' }))
  const Host = defineComponent({
    components: { FormLanguageSwitch },
    setup() {
      trans = useFormTranslations({ apiFetch: useApi().apiFetch, urls: { list: () => '', save: () => '' }, formData, canEdit: ref(true), titleRequired })
      return () => h(FormLanguageSwitch, { trans })
    },
  })
  wrapper = mount(Host, { global: { plugins: [i18n], stubs: onyxFormStubs } })
  await trans.load(7)
  await flushPromises()
}
const translateCalls = () => fetchMock.mock.calls.filter(([u]) => /translate$/.test(u))
const ok = (target) => json({ title: `T-${target}`, message: `M-${target}`, provider: 'libretranslate' })

describe('translateAll', () => {
  beforeEach(() => { i18n.global.locale.value = 'de'; vi.spyOn(console, 'error').mockImplementation(() => {}) })
  afterEach(() => { wrapper?.unmount(); vi.restoreAllMocks() })

  it('übersetzt alle leeren Sprachen als Entwurf und lässt gefüllte unberührt', async () => {
    await setup(async (target) => ok(target))
    Object.assign(trans.draftOf('fr'), { title: 'Mein Titel', message: 'Mein Text', done: true })
    await flushPromises()
    expect(wrapper.find('[data-test="lang-translate-all"]').text()).toContain('2')
    await wrapper.find('[data-test="lang-translate-all"]').trigger('click')
    await flushPromises()

    expect(translateCalls().map(([, o]) => JSON.parse(o.body).target_language)).toEqual(['en', 'it'])
    expect(trans.draftOf('en')).toMatchObject({ title: 'T-en', message: 'M-en', done: false })
    expect(trans.draftOf('fr')).toMatchObject({ title: 'Mein Titel', message: 'Mein Text', done: true })
    expect(wrapper.find('[data-test="lang-translate-all-summary"]').text()).toContain('2 Sprachen übersetzt')
    expect(trans.translatingAll.value).toBe(false)
  })

  it('überspringt eine nicht unterstützte Sprache und macht weiter', async () => {
    await setup(async (target) => (target === 'en' ? json({ code: 'translation_language_unsupported' }, 422) : ok(target)))
    await trans.translateAll()
    expect(translateCalls()).toHaveLength(3)
    expect(trans.stateOf('en')).toBe('empty')
    expect(trans.stateOf('fr')).toBe('draft')
    expect(trans.translateAllSummary.value).toContain('2 Sprachen übersetzt')
    expect(trans.translateAllSummary.value).toContain('1 übersprungen')
  })

  it.each([
    ['Kontingent', async () => json({ code: 'translation_quota_exceeded' }, 429), 'Kontingent'],
    ['Zugang abgelehnt', async () => json({ code: 'translation_auth_failed' }, 502), 'Zugang'],
    ['Dienst nicht erreichbar', async () => json({ code: 'translation_unavailable' }, 502), 'erreichbar'],
    ['Netz weg (fetch wirft)', async () => { throw new Error('Failed to fetch') }, 'Server ist nicht erreichbar'],
    ['Sitzung abgelaufen (401)', async () => json({ error: 'x' }, 401), null],
    ['Verboten (403) ohne Code', async () => json({}, 403), null],
    ['5xx ohne JSON-Körper', async () => ({ ok: false, status: 500, json: async () => { throw new Error('kein JSON') } }), null],
  ])('bricht bei %s nach der ersten Sprache ab und nennt den Fehler', async (_n, handler, text) => {
    await setup(async (target) => (target === 'en' ? ok(target) : handler()))
    // en gelingt, fr scheitert: it wird nicht mehr angefragt.
    await trans.translateAll()
    expect(translateCalls().map(([, o]) => JSON.parse(o.body).target_language)).toEqual(['en', 'fr'])
    const summary = trans.translateAllSummary.value
    expect(summary).toContain('Abgebrochen nach 1 Sprachen')
    expect(summary).not.toContain('übersprungen')
    if (text) expect(summary).toContain(text)
    expect(trans.translatingAll.value).toBe(false)
  })

  it('bricht bei einem Fehler in der ersten Sprache sofort ab (keine aussichtslosen Anfragen)', async () => {
    await setup(async () => { throw new Error('offline') })
    await trans.translateAll()
    expect(translateCalls()).toHaveLength(1)
    expect(trans.translateAllSummary.value).toContain('Server ist nicht erreichbar')
    expect(trans.translateAllSummary.value).not.toContain('offline')
  })

  it('ist ohne leere Sprachen oder ohne Text in der Standardsprache gesperrt', async () => {
    await setup(async (target) => ok(target), { data: { Title: '  ', Message: 'x' } })
    expect(trans.canTranslateAll.value).toBe(false)
    await trans.translateAll()
    expect(translateCalls()).toHaveLength(0)
    formData.value.Title = 'Titel'
    for (const c of ['en', 'fr', 'it']) Object.assign(trans.draftOf(c), { title: 'a', message: 'b' })
    expect(trans.canTranslateAll.value).toBe(false)
    await flushPromises()
    expect(wrapper.find('[data-test="lang-translate-all"]').exists()).toBe(false)
  })

  it('sendet für Updates ohne Titel keinen Titel und braucht nur die Nachricht', async () => {
    await setup(async (target) => json({ title: '', message: `M-${target}` }), { titleRequired: false })
    await trans.translateAll()
    const bodies = translateCalls().map(([, o]) => JSON.parse(o.body))
    expect(bodies).toHaveLength(3)
    for (const b of bodies) expect(b).not.toHaveProperty('title')
    expect(trans.draftOf('en')).toMatchObject({ title: '', message: 'M-en' })

    formData.value.Message = ''
    for (const c of ['en', 'fr', 'it']) trans.draftOf(c).message = ''
    expect(trans.canTranslateAll.value).toBe(false)
  })

  it('sendet eine fehlende Nachricht der Standardsprache als leeren Text', async () => {
    await setup(async (target) => ok(target), { data: { Title: 'Nur Titel', Message: null } })
    await trans.translateAll()
    for (const [, o] of translateCalls()) expect(JSON.parse(o.body).message).toBe('')
    expect(translateCalls()).toHaveLength(3)
  })
})
