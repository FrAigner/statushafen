import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, ref, type Ref } from 'vue'
import i18n from '../i18n'
import { onyxFormStubs } from '../test-support/onyxStubs'
import { useApi } from './useApi'
import { useFormTranslations, type FormTranslations } from './useFormTranslations'
import { fetchMock as createFetchMock, type ResponseStub } from '../test-support/fetchStub'
import FormLanguageSwitch from '../components/FormLanguageSwitch.vue'

// "Translate" button in the language switcher (composable + component).

const json = (body: unknown, status = 200): ResponseStub => ({ ok: status < 400, status, json: async () => body })
const catalog = [{ Code: 'de', Name: 'Deutsch' }, { Code: 'en', Name: 'English' }, { Code: 'fr', Name: 'Français' }]
const pageLangs = [{ LanguageCode: 'de', IsDefault: true }, { LanguageCode: 'en', IsDefault: false }, { LanguageCode: 'fr', IsDefault: false }]

type FormFields = { Title: string; Message: string | null }
type TranslateHandler = (url: string, o: RequestInit) => Promise<ResponseStub>

let wrapper: VueWrapper
let trans: FormTranslations
let formData: Ref<FormFields>

const isTranslate = ([u]: [RequestInfo | URL, RequestInit?]) => /translate$/.test(String(u))
const bodyOf = (o: RequestInit | undefined) => JSON.parse(String(o?.body))

async function setup(translateHandler: TranslateHandler, { title = 'Störung', message = 'Details' }: { title?: string; message?: string | null } = {}) {
  const fetchMock = createFetchMock(async (input, o = {}) => {
    const url = String(input)
    if (url === '/api/languages') return json(catalog)
    if (/\/languages$/.test(url)) return json(pageLangs)
    if (/\/translate$/.test(url)) return translateHandler(url, o)
    throw new Error(`unerwartet ${url}`)
  })
  global.fetch = fetchMock
  formData = ref<FormFields>({ Title: title, Message: message })
  const Host = defineComponent({
    components: { FormLanguageSwitch },
    setup() {
      trans = useFormTranslations({
        apiFetch: useApi().apiFetch,
        urls: { list: () => '', save: () => '' }, formData, canEdit: ref(true),
      })
      return () => h(FormLanguageSwitch, { trans })
    },
  })
  wrapper = mount(Host, { global: { plugins: [i18n], stubs: onyxFormStubs } })
  await trans.load(7)
  await flushPromises()
  return fetchMock
}

const button = () => wrapper.find('[data-test="lang-translate"]')

describe('Übersetzen-Knopf', () => {
  beforeEach(() => { i18n.global.locale.value = 'de'; vi.spyOn(console, 'error').mockImplementation(() => {}) })
  afterEach(() => { wrapper?.unmount(); vi.restoreAllMocks() })

  it('ist in der Standardsprache nicht da', async () => {
    await setup(async () => json({}))
    expect(button().exists()).toBe(false)
  })

  it('übersetzt in die gewählte Sprache, setzt einen Entwurf (nicht fertig) und sendet Titel und Nachricht', async () => {
    const fetchMock = await setup(async () => json({ title: 'Outage', message: 'Details EN', provider: 'libretranslate', source_language: 'de' }))
    trans.active.value = 'en'
    trans.draftOf('en').done = true
    await flushPromises()
    expect(button().text()).toContain('Deutsch')
    await button().trigger('click')
    await flushPromises()

    const [url, o] = fetchMock.mock.calls.find(isTranslate)!
    expect(url).toBe('/api/pages/7/translate')
    expect(bodyOf(o)).toEqual({ target_language: 'en', title: 'Störung', message: 'Details' })
    expect(trans.draftOf('en')).toMatchObject({ title: 'Outage', message: 'Details EN', done: false })
    expect(trans.stateOf('en')).toBe('draft')
    expect(wrapper.find('[data-test="lang-translate-error"]').exists()).toBe(false)
  })

  it('weist darauf hin, wenn der Vorschlag vom Ersatzdienst kam, und nimmt den Hinweis beim Sprachwechsel zurück', async () => {
    await setup(async () => json({ title: 'Outage', message: 'Body', provider: 'libretranslate', fallback: true }))
    trans.active.value = 'en'
    await flushPromises()
    await button().trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="lang-translate-fallback"]').text()).toContain('Ersatzdienst')

    trans.active.value = 'fr'
    await flushPromises()
    expect(wrapper.find('[data-test="lang-translate-fallback"]').exists()).toBe(false)
  })

  it('zeigt ohne Ersatzdienst keinen Hinweis', async () => {
    await setup(async () => json({ title: 'Outage', message: 'Body', provider: 'deepl', fallback: false }))
    trans.active.value = 'en'
    await flushPromises()
    await button().trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="lang-translate-fallback"]').exists()).toBe(false)
  })

  it('schickt eine leere Nachricht als leeren Text', async () => {
    const fetchMock = await setup(async () => json({ title: 'T', message: '' }), { message: null })
    trans.active.value = 'en'
    await trans.translate()
    expect(bodyOf(fetchMock.mock.calls.find(isTranslate)![1]).message).toBe('')
  })

  it('ist ohne Titel in der Standardsprache gesperrt und ruft nichts auf', async () => {
    const fetchMock = await setup(async () => json({}), { title: '  ' })
    trans.active.value = 'en'
    await flushPromises()
    expect(button().attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('Titel in der Standardsprache')
    await trans.translate()
    expect(fetchMock.mock.calls.some(isTranslate)).toBe(false)
  })

  it.each([
    ['translation_not_configured', 503, 'nicht eingerichtet'],
    ['translation_unavailable', 502, 'nicht erreichbar'],
    ['translation_language_unsupported', 422, 'unterstützt diese Sprache nicht'],
    ['translation_quota_exceeded', 429, 'Kontingent'],
  ])('meldet %s ohne den Entwurf zu verändern', async (code, status, text) => {
    await setup(async () => json({ code }, status))
    trans.active.value = 'fr'
    trans.draftOf('fr').title = 'Vorhandener Titel'
    await flushPromises()
    await button().trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="lang-translate-error"]').text()).toContain(text)
    expect(trans.draftOf('fr').title).toBe('Vorhandener Titel')
    expect(trans.translating.value).toBe(false)
    // switching the language clears the message
    trans.active.value = 'en'
    await flushPromises()
    expect(wrapper.find('[data-test="lang-translate-error"]').exists()).toBe(false)
  })

  it('ignoriert einen zweiten Klick während der Übersetzung und zeigt „Übersetze …“', async () => {
    let release: () => void = () => {}
    const fetchMock = await setup(() => new Promise<ResponseStub>((resolve) => { release = () => resolve(json({ title: 'a', message: 'b' })) }))
    trans.active.value = 'en'
    await flushPromises()
    const first = trans.translate()
    await flushPromises()
    expect(button().text()).toContain('Übersetze')
    await trans.translate()
    release()
    await first
    expect(fetchMock.mock.calls.filter(isTranslate)).toHaveLength(1)
  })

  it('reset löscht die Fehlermeldung', async () => {
    await setup(async () => json({ code: 'translation_unavailable' }, 502))
    trans.active.value = 'en'
    await trans.translate()
    expect(trans.translateError.value).not.toBe('')
    trans.reset()
    expect(trans.translateError.value).toBe('')
  })
})
