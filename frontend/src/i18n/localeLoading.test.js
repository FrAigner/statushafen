import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import i18n, {
  SUPPORTED_LOCALES, LANGUAGE_STORAGE_KEY, registerLocale, registerLocalesFromGlob, loadLocaleMessages, isLocaleLoaded, detectInitialLocale
} from './index'
import { applyGlobalOverrides } from './overrides'
import { useLanguage } from '../composables/useLanguage'

// Teil 1: Eine neue Datei locales/<code>.json wird ohne Codeänderung als
// Oberflächensprache erkannt und beim Wechsel nachgeladen. Die Fixture-Sprache "zz"
// ersetzt eine echte Datei (echte weitere Sprachen kommen mit).
const ZZ = { common: { save: 'ZZ-Speichern' }, nav: { items: { home: 'ZZ-Start' } } }

function mountLanguage() {
  let result
  mount({ setup() { result = useLanguage(); return () => null } }, { global: { plugins: [i18n] } })
  return result
}

describe('Nachladbare Oberflächensprachen', () => {
  let resolveLoader
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
    registerLocale('zz', () => null)
    expect(SUPPORTED_LOCALES.filter((c) => c === 'zz')).toHaveLength(1)
  })

  it('leitet den Sprachcode aus dem Dateinamen der Glob-Treffer ab', async () => {
    registerLocalesFromGlob({ './locales/zu.json': async () => ({ default: { common: { save: 'ZU' } } }) })
    expect(SUPPORTED_LOCALES).toContain('zu')
    expect(await loadLocaleMessages('zu')).toBe(true)
    expect(i18n.global.t('common.save', {}, { locale: 'zu' })).toBe('ZU')
    SUPPORTED_LOCALES.splice(SUPPORTED_LOCALES.indexOf('zu'), 1)
  })

  it('lädt die Texte nach, bevor die Locale gesetzt wird, und nutzt sie danach', async () => {
    const { setLanguage, languageOptions } = mountLanguage()
    expect(languageOptions.value.map((o) => o.code)).toContain('zz')
    expect(languageOptions.value.find((o) => o.code === 'zz').label).toBe('zz')
    const pending = setLanguage('zz')
    await flushPromises()
    expect(i18n.global.locale.value).toBe('de')
    expect(isLocaleLoaded('zz')).toBe(false)
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, json: async () => ({}) })
    resolveLoader()
    await pending
    expect(i18n.global.locale.value).toBe('zz')
    expect(i18n.global.t('common.save')).toBe('ZZ-Speichern')
    expect(i18n.global.t('nav.items.home')).toBe('ZZ-Start')
    // fehlende Schlüssel fallen weiter auf die Standardtexte zurück
    expect(i18n.global.t('common.cancel')).toBe(i18n.global.t('common.cancel', {}, { locale: 'de' }))
  })

  it('wendet globale Overrides auf den nachgeladenen Texten an', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, json: async () => ({ 'common.save': 'Eigener Text' }) })
    // frische Sprache, deren Texte noch nicht geladen sind (zz ist durch frühere Tests geladen)
    registerLocale('zt', async () => ({ default: ZZ }))
    expect(isLocaleLoaded('zt')).toBe(false)
    await applyGlobalOverrides('zt')
    expect(i18n.global.t('common.save', {}, { locale: 'zt' })).toBe('Eigener Text')
    expect(i18n.global.t('nav.items.home', {}, { locale: 'zt' })).toBe('ZZ-Start')
    SUPPORTED_LOCALES.splice(SUPPORTED_LOCALES.indexOf('zt'), 1)
  })

  it('meldet false bei unbekannter Sprache und bei Ladefehler', async () => {
    expect(await loadLocaleMessages('qq')).toBe(false)
    registerLocale('zy', () => Promise.reject(new Error('offline')))
    expect(await loadLocaleMessages('zy')).toBe(false)
    SUPPORTED_LOCALES.splice(SUPPORTED_LOCALES.indexOf('zy'), 1)
  })
})
