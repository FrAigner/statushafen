import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import i18n from '../i18n'
import { useLanguage } from './useLanguage'
import { usePublicPageLanguages } from './usePublicPageLanguages'

const LANGS = [
  { code: 'cs', name: 'Čeština', is_default: false },
  { code: 'de', name: 'Deutsch', is_default: true },
  { code: 'hr', name: 'Hrvatski', is_default: false },
]

function mountLanguage() {
  let result
  mount({ setup() { result = useLanguage(); return () => null } }, { global: { plugins: [i18n] } })
  return result
}

// Der Sprachumschalter einer Statusseite bot nur de/en an, weil er
// die Sprachen der Seite nicht kannte.
describe('usePublicPageLanguages', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'de'
    localStorage.clear()
    usePublicPageLanguages().clearPageLanguages()
  })

  it('bietet ohne Seitensprachen die Oberflächensprachen an', () => {
    const { languageOptions } = mountLanguage()
    expect(languageOptions.value.map((o) => o.code)).toEqual(['de', 'en'])
  })

  it('bietet mit Seitensprachen genau diese an, mit Eigenbezeichnung', () => {
    usePublicPageLanguages().setPageLanguages(LANGS)
    const { languageOptions } = mountLanguage()
    expect(languageOptions.value).toEqual([
      { code: 'cs', label: 'Čeština' }, { code: 'de', label: 'Deutsch' }, { code: 'hr', label: 'Hrvatski' },
    ])
  })

  it('nimmt ungültige Listen und Einträge nicht an und nutzt den Code ohne Namen', () => {
    const { setPageLanguages } = usePublicPageLanguages()
    setPageLanguages([null, { code: '' }, { name: 'x' }, { code: 'hr' }])
    expect(mountLanguage().languageOptions.value).toEqual([{ code: 'hr', label: 'hr' }])
    setPageLanguages('kaputt')
    expect(mountLanguage().languageOptions.value.map((o) => o.code)).toEqual(['de', 'en'])
  })

  it('wechselt auf eine Seitensprache ohne Oberflächentexte, ohne Profil und Overrides anzufragen', async () => {
    usePublicPageLanguages().setPageLanguages(LANGS)
    global.fetch = vi.fn()
    const { setLanguage } = mountLanguage()
    await setLanguage('hr')
    expect(i18n.global.locale.value).toBe('hr')
    expect(localStorage.getItem('preferred_language')).toBe('hr')
    expect(global.fetch).not.toHaveBeenCalled()
  })

  it('lehnt Sprachen ab, die weder die Seite noch die Oberfläche anbietet', async () => {
    usePublicPageLanguages().setPageLanguages(LANGS)
    const { setLanguage } = mountLanguage()
    await setLanguage('en')
    expect(i18n.global.locale.value).toBe('de')
  })

  it('zeigt die Standardsprache der Seite, wenn die Locale nicht angeboten wird', () => {
    i18n.global.locale.value = 'en'
    usePublicPageLanguages().setPageLanguages(LANGS)
    expect(mountLanguage().selectedLocale.value).toBe('de')
    i18n.global.locale.value = 'hr'
    expect(mountLanguage().selectedLocale.value).toBe('hr')
  })

  it('behält die Locale ohne Standardmarkierung', () => {
    i18n.global.locale.value = 'en'
    usePublicPageLanguages().setPageLanguages([{ code: 'fr', name: 'Français' }])
    expect(mountLanguage().selectedLocale.value).toBe('en')
  })

  it('stellt eine gespeicherte Seitensprache wieder her', () => {
    localStorage.setItem('preferred_language', 'hr')
    usePublicPageLanguages().setPageLanguages(LANGS)
    expect(i18n.global.locale.value).toBe('hr')
  })

  it.each([
    ['eine Sprache, die die Seite nicht hat', 'fr'],
    ['eine Oberflächensprache', 'en'],
    ['nichts gespeichert', null],
  ])('stellt bei %s nichts wieder her', (_n, stored) => {
    if (stored) localStorage.setItem('preferred_language', stored)
    usePublicPageLanguages().setPageLanguages(LANGS)
    expect(i18n.global.locale.value).toBe('de')
  })

  it('setzt beim Verlassen der Seite eine Sprache ohne Oberflächentexte zurück und leert die Liste', () => {
    usePublicPageLanguages().setPageLanguages(LANGS)
    i18n.global.locale.value = 'hr'
    localStorage.setItem('preferred_language', 'hr')
    const { pageLanguages, clearPageLanguages } = usePublicPageLanguages()
    clearPageLanguages()
    expect(pageLanguages.value).toEqual([])
    expect(['de', 'en']).toContain(i18n.global.locale.value)
  })

  it('lässt beim Verlassen eine Oberflächensprache unverändert', () => {
    i18n.global.locale.value = 'en'
    usePublicPageLanguages().clearPageLanguages()
    expect(i18n.global.locale.value).toBe('en')
  })
})

// Teil 1: Rückfallkette für Seitensprachen ohne Oberflächentexte:
// Standardsprache der Seite (falls mit Texten) → Englisch → Deutsch. Vorher fiel
// vue-i18n immer auf Deutsch zurück.
describe('Rückfallkette der Oberflächentexte', () => {
  const page = (defaultCode) => [
    { code: 'de', name: 'Deutsch', is_default: defaultCode === 'de' },
    { code: 'en', name: 'English', is_default: defaultCode === 'en' },
    { code: 'fr', name: 'Français', is_default: defaultCode === 'fr' },
    { code: 'hr', name: 'Hrvatski', is_default: defaultCode === 'hr' },
  ]
  const textFor = (locale) => i18n.global.t('common.save', {}, { locale })

  beforeEach(() => {
    i18n.global.locale.value = 'de'
    localStorage.clear()
    usePublicPageLanguages().clearPageLanguages()
  })

  it.each([
    ['de', ['de', 'en']],
    ['en', ['en', 'de']],
    ['fr', ['en', 'de']],
  ])('Standardsprache %s der Seite ergibt die Kette %j', (def, chain) => {
    usePublicPageLanguages().setPageLanguages(page(def))
    expect(i18n.global.fallbackLocale.value).toEqual(chain)
  })

  it('Seitensprache ohne Texte nutzt Deutsch, wenn die Seite Deutsch als Standard hat', () => {
    usePublicPageLanguages().setPageLanguages(page('de'))
    expect(textFor('hr')).toBe(textFor('de'))
  })

  it('Seitensprache ohne Texte nutzt Englisch, wenn die Standardsprache der Seite keine Texte hat', () => {
    usePublicPageLanguages().setPageLanguages(page('fr'))
    expect(textFor('hr')).toBe(textFor('en'))
    expect(textFor('hr')).not.toBe(textFor('de'))
  })

  it('Seitensprache ohne Texte nutzt die englische Standardsprache der Seite', () => {
    usePublicPageLanguages().setPageLanguages(page('en'))
    expect(textFor('hr')).toBe(textFor('en'))
  })

  it('ohne Standardmarkierung gilt Englisch → Deutsch', () => {
    usePublicPageLanguages().setPageLanguages([{ code: 'hr', name: 'Hrvatski' }])
    expect(i18n.global.fallbackLocale.value).toEqual(['en', 'de'])
  })

  it('beim Verlassen der Seite gilt wieder der Rückfall Deutsch (Admin-Bereich unverändert)', () => {
    usePublicPageLanguages().setPageLanguages(page('fr'))
    usePublicPageLanguages().clearPageLanguages()
    expect(i18n.global.fallbackLocale.value).toBe('de')
  })
})
