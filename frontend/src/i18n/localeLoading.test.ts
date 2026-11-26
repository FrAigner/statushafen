import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n, {
  SUPPORTED_LOCALES, LANGUAGE_STORAGE_KEY, registerLocale, registerLocalesFromGlob, loadLocaleMessages, isLocaleLoaded, detectInitialLocale
} from './index'
import { applyGlobalOverrides } from './overrides'
import { useLanguage } from '../composables/useLanguage'
import { asResponse } from '../test-support/fetchStub'
import type { LocaleLoader } from './index'

// Lazily loaded languages are valid locales at runtime.
type Locale = typeof i18n.global.locale.value

// Part 1: a new file locales/<code>.json is recognized as a UI language
// without a code change and loaded lazily on switching. The fixture language "zz"
// stands in for a real file (real further languages come along).
const ZZ = { common: { save: 'ZZ-Speichern' }, nav: { items: { home: 'ZZ-Start' } } }

function mountLanguage() {
  let result!: ReturnType<typeof useLanguage>
  mount({ setup() { result = useLanguage(); return () => null } }, { global: { plugins: [i18n] } })
  return result
}

describe('Nachladbare Oberflächensprachen', () => {
  let resolveLoader!: () => void
  beforeEach(() => {
    localStorage.clear()
    i18n.global.locale.value = 'de'
    registerLocale('zz', () => new Promise((resolve) => { resolveLoader = () => resolve({ default: ZZ }) }))
  })
  afterEach(() => {
    SUPPORTED_LOCALES.splice(SUPPORTED_LOCALES.indexOf('zz'), 1)
    i18n.global.locale.value = 'de'
    vi.restoreAllMocks()
  })

  it('erkennt die Sprache als Oberflächensprache, de und en bleiben vorn', () => {
    expect(SUPPORTED_LOCALES).toEqual(['de', 'en', 'zz'])
    localStorage.setItem(LANGUAGE_STORAGE_KEY, 'zz')
    expect(detectInitialLocale()).toBe('zz')
    // Never called: only the registration matters.
    registerLocale('zz', (() => null) as unknown as LocaleLoader)
    expect(SUPPORTED_LOCALES.filter((c) => c === 'zz')).toHaveLength(1)
  })

  it('leitet den Sprachcode aus dem Dateinamen der Glob-Treffer ab', async () => {
    registerLocalesFromGlob({ './locales/zu.json': async () => ({ default: { common: { save: 'ZU' } } }) })
    expect(SUPPORTED_LOCALES).toContain('zu')
    expect(await loadLocaleMessages('zu')).toBe(true)
    expect(i18n.global.t('common.save', {}, { locale: 'zu' as Locale })).toBe('ZU')
    SUPPORTED_LOCALES.splice(SUPPORTED_LOCALES.indexOf('zu'), 1)
  })

  it('lädt die Texte nach, bevor die Locale gesetzt wird, und nutzt sie danach', async () => {
    const { setLanguage, languageOptions } = mountLanguage()
    expect(languageOptions.value.map((o) => o.code)).toContain('zz')
    expect(languageOptions.value.find((o) => o.code === 'zz')!.label).toBe('zz')
    const pending = setLanguage('zz')
    await flushPromises()
    expect(i18n.global.locale.value).toBe('de')
    expect(isLocaleLoaded('zz')).toBe(false)
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(asResponse({ ok: true, json: async () => ({}) }))
    resolveLoader()
    await pending
    expect(i18n.global.locale.value).toBe('zz')
    expect(i18n.global.t('common.save')).toBe('ZZ-Speichern')
    expect(i18n.global.t('nav.items.home')).toBe('ZZ-Start')
    // missing keys still fall back to the default texts
    expect(i18n.global.t('common.cancel')).toBe(i18n.global.t('common.cancel', {}, { locale: 'de' }))
  })

  it('wendet globale Overrides auf den nachgeladenen Texten an', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(asResponse({ ok: true, json: async () => ({ 'common.save': 'Eigener Text' }) }))
    // a fresh language whose texts are not loaded yet (zz is loaded by earlier tests)
    registerLocale('zt', async () => ({ default: ZZ }))
    expect(isLocaleLoaded('zt')).toBe(false)
    await applyGlobalOverrides('zt')
    expect(i18n.global.t('common.save', {}, { locale: 'zt' as Locale })).toBe('Eigener Text')
    expect(i18n.global.t('nav.items.home', {}, { locale: 'zt' as Locale })).toBe('ZZ-Start')
    SUPPORTED_LOCALES.splice(SUPPORTED_LOCALES.indexOf('zt'), 1)
  })

  it('meldet false bei unbekannter Sprache und bei Ladefehler', async () => {
    expect(await loadLocaleMessages('qq')).toBe(false)
    registerLocale('zy', () => Promise.reject(new Error('offline')))
    expect(await loadLocaleMessages('zy')).toBe(false)
    SUPPORTED_LOCALES.splice(SUPPORTED_LOCALES.indexOf('zy'), 1)
  })
})

describe('detectInitialLocale', () => {
  beforeEach(() => { localStorage.clear() })
  afterEach(() => { vi.restoreAllMocks() })

  it('übernimmt eine unterstützte Browsersprache', () => {
    vi.spyOn(navigator, 'language', 'get').mockReturnValue('en-GB')
    expect(detectInitialLocale()).toBe('en')
  })

  it('fällt bei nicht unterstützter oder fehlender Browsersprache auf die Standardsprache zurück', () => {
    const lang = vi.spyOn(navigator, 'language', 'get').mockReturnValue('fr-FR')
    expect(detectInitialLocale()).toBe('de')
    lang.mockReturnValue('')
    expect(detectInitialLocale()).toBe('de')
  })
})
