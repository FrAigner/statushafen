import { createI18n } from 'vue-i18n'
import de from './locales/de.json'
import en from './locales/en.json'

// de und en sind eager im Bundle (Standard und Rückfall). Jede weitere Datei
// src/i18n/locales/<code>.json wird ohne Codeänderung als Oberflächensprache
// erkannt und beim Wechsel nachgeladen (Teil 1).
export const SUPPORTED_LOCALES = ['de', 'en']
const localeLoaders = {}
const baseMessages = { de, en }

export function registerLocale(code, loader) {
  localeLoaders[code] = loader
  if (!SUPPORTED_LOCALES.includes(code)) SUPPORTED_LOCALES.push(code)
}

export function registerLocalesFromGlob(modules) {
  for (const [path, loader] of Object.entries(modules)) {
    registerLocale(/([^/]+)\.json$/.exec(path)[1], loader)
  }
}
registerLocalesFromGlob(import.meta.glob(['./locales/*.json', '!./locales/de.json', '!./locales/en.json']))

export const getBaseMessages = (code) => baseMessages[code]
export const isLocaleLoaded = (code) => Object.hasOwn(baseMessages, code)

// Lädt die Texte einer Oberflächensprache nach (muss vor dem Setzen der Locale
// fertig sein). Liefert false, wenn es keine Datei gibt oder das Laden scheitert.
export async function loadLocaleMessages(code) {
  if (isLocaleLoaded(code)) return true
  if (!Object.hasOwn(localeLoaders, code)) return false
  try {
    const mod = await localeLoaders[code]()
    baseMessages[code] = mod.default
    i18n.global.setLocaleMessage(code, baseMessages[code])
    return true
  } catch {
    return false
  }
}
export const DEFAULT_LOCALE = 'de'
export const LANGUAGE_STORAGE_KEY = 'preferred_language'

// Ermittelt die initiale Sprache (i18n-Infrastruktur):
// 1. Bereits im localStorage gespeicherte Wahl (anonymer Besucher oder
//    zuvor eingeloggter User, dessen Profil-Sprache beim Login übernommen wurde)
// 2. Browsersprache, sofern unterstützt
// 3. Fallback DEFAULT_LOCALE, da die bestehende UI überwiegend Deutsch ist
export function detectInitialLocale() {
  const stored = localStorage.getItem(LANGUAGE_STORAGE_KEY)
  if (stored && SUPPORTED_LOCALES.includes(stored)) return stored

  const browserLang = (navigator.language || '').slice(0, 2)
  if (SUPPORTED_LOCALES.includes(browserLang)) return browserLang

  return DEFAULT_LOCALE
}

// Teil 1: Rückfallkette der Oberflächentexte für Seitensprachen
// ohne eigene Texte (z. B. hr, fr): Standardsprache der Seite, falls sie
// Oberflächentexte hat → Englisch → Deutsch. Ohne Statusseite (Admin-Bereich)
// bleibt es beim Rückfall DEFAULT_LOCALE.
export function buildFallbackChain(pageDefault) {
  const chain = SUPPORTED_LOCALES.includes(pageDefault) ? [pageDefault] : []
  return [...new Set([...chain, 'en', DEFAULT_LOCALE])]
}

const i18n = createI18n({
  legacy: false,
  globalInjection: true,
  locale: detectInitialLocale(),
  fallbackLocale: DEFAULT_LOCALE,
  messages: { de, en }
})

export default i18n
