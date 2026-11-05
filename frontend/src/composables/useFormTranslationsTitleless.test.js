import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import i18n from '../i18n'
import { onyxFormStubs } from '../test-support/onyxStubs'
import { useApi } from './useApi'
import { useFormTranslations } from './useFormTranslations'
import FormLanguageSwitch from '../components/FormLanguageSwitch.vue'

// titleRequired=false (Updates haben nur eine Nachricht). Das Verhalten mit
// Titel decken useFormTranslationsTranslate.test.js und die Ansichts-Tests ab.

const json = (body, status = 200) => ({ ok: status < 400, status, json: async () => body })
const catalog = [{ Code: 'de', Name: 'Deutsch' }, { Code: 'en', Name: 'English' }, { Code: 'fr', Name: 'Français' }]
const pageLangs = [{ LanguageCode: 'de', IsDefault: true }, { LanguageCode: 'en', IsDefault: false }, { LanguageCode: 'fr', IsDefault: false }]

let wrapper
let trans
let formData
let fetchMock

async function setup({ message = 'Wir arbeiten daran', listRows = [], translateBody = { title: 'IGNORIERT', message: 'We are working on it' } } = {}) {
  fetchMock = vi.fn(async (url, o = {}) => {
    if (url === '/api/languages') return json(catalog)
    if (/\/languages$/.test(url)) return json(pageLangs)
    if (/\/translate$/.test(url)) return json(translateBody)
    if (url === 'list') return json(listRows)
    if (/^save\//.test(url)) return json({})
    throw new Error(`unerwartet ${url} ${o.method}`)
  })
  global.fetch = fetchMock
  formData = ref({ message })
  const Host = defineComponent({
    components: { FormLanguageSwitch },
    setup() {
      trans = useFormTranslations({
        apiFetch: useApi().apiFetch,
        urls: { list: () => 'list', save: (id, code) => `save/${id}/${code}` },
        formData, canEdit: ref(true), contentKey: 'message', titleRequired: false,
      })
      return () => h(FormLanguageSwitch, { trans })
    },
  })
  wrapper = mount(Host, { global: { plugins: [i18n], stubs: onyxFormStubs } })
  await trans.load(7)
  await flushPromises()
}

const saves = () => fetchMock.mock.calls.filter(([u]) => /^save\//.test(u))

describe('useFormTranslations ohne Titel', () => {
  beforeEach(() => { i18n.global.locale.value = 'de'; vi.spyOn(console, 'error').mockImplementation(() => {}) })
  afterEach(() => { wrapper?.unmount(); vi.restoreAllMocks() })

  it('gilt eine Sprache mit Nachricht als ausgefüllt und speichert ohne title', async () => {
    await setup()
    expect(trans.titleless).toBe(true)
    trans.draftOf('en').message = 'Text EN'
    expect(trans.stateOf('en')).toBe('draft')
    trans.draftOf('en').done = true
    expect(trans.stateOf('en')).toBe('done')
    expect(trans.stateOf('fr')).toBe('empty')
    expect(trans.incompleteCode()).toBeNull()
    await trans.saveAll(5)
    expect(saves()).toHaveLength(1)
    expect(saves()[0][0]).toBe('save/5/en')
    expect(JSON.parse(saves()[0][1].body)).toEqual({ message: 'Text EN', status: 'confirmed' })
  })

  it('lädt vorhandene Übersetzungen ohne Titelfeld', async () => {
    await setup({ listRows: [{ LanguageCode: 'fr', Message: 'Texte', Status: 'draft' }] })
    await trans.loadFor(9)
    expect(trans.draftOf('fr')).toMatchObject({ title: '', message: 'Texte', done: false })
    expect(trans.stateOf('fr')).toBe('draft')
  })

  it('übersetzt nur die Nachricht: Knopf braucht die Nachricht, gesendet wird kein Titel, ein gelieferter Titel wird verworfen', async () => {
    await setup({ message: '' })
    trans.active.value = 'en'
    await flushPromises()
    expect(trans.canTranslate.value).toBe(false)
    expect(wrapper.text()).toContain('zuerst die Nachricht in der Standardsprache')
    expect(wrapper.text()).not.toContain('zuerst den Titel')

    formData.value.message = 'Wir arbeiten daran'
    expect(trans.canTranslate.value).toBe(true)
    await trans.translate()
    const [, o] = fetchMock.mock.calls.find(([u]) => /translate$/.test(u))
    expect(JSON.parse(o.body)).toEqual({ target_language: 'en', message: 'Wir arbeiten daran' })
    expect(trans.draftOf('en')).toMatchObject({ title: '', message: 'We are working on it', done: false })
  })
})
