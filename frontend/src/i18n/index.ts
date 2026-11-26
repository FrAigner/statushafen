import { createI18n } from 'vue-i18n'
import de from './locales/de.json'
import en from './locales/en.json'

// de and en are eagerly in the bundle (default and fallback). Every further file
// src/i18n/locales/<code>.json is recognized as a UI language without a code change
// and loaded lazily on switching (part 1).

/** Messages of one UI language (the shape of de.json; lazily loaded files follow it). */
export type LocaleMessages = typeof de

/** Lazy import of a locale file as import.meta.glob returns it. */
export type LocaleLoader = () => Promise<unknown>

export const SUPPORTED_LOCALES: string[] = ['de', 'en']
const localeLoaders: Record<string, LocaleLoader> = {}
const baseMessages: Record<string, LocaleMessages> = { de, en }

export function registerLocale(code: string, loader: LocaleLoader): void {
  localeLoaders[code] = loader
  if (!SUPPORTED_LOCALES.includes(code)) SUPPORTED_LOCALES.push(code)
}

export function registerLocalesFromGlob(modules: Record<string, LocaleLoader>): void {
  for (const [path, loader] of Object.entries(modules)) {
    registerLocale((/([^/]+)\.json$/.exec(path) as RegExpExecArray)[1] as string, loader)
  }
}
registerLocalesFromGlob(import.meta.glob(['./locales/*.json', '!./locales/de.json', '!./locales/en.json']))

export const getBaseMessages = (code: string): LocaleMessages | undefined => baseMessages[code]
export const isLocaleLoaded = (code: string): boolean => Object.hasOwn(baseMessages, code)

// Loads the texts of a UI language lazily (must be done before the locale is
// set). Returns false if there is no file or loading fails.
export async function loadLocaleMessages(code: string): Promise<boolean> {
  if (isLocaleLoaded(code)) return true
  if (!Object.hasOwn(localeLoaders, code)) return false
  try {
    const mod = await (localeLoaders[code] as LocaleLoader)() as { default: LocaleMessages }
    baseMessages[code] = mod.default
    i18n.global.setLocaleMessage(code, baseMessages[code] as LocaleMessages)
    return true
  } catch {
    return false
  }
}
export const DEFAULT_LOCALE = 'de'
export const LANGUAGE_STORAGE_KEY = 'preferred_language'

// Determines the initial language (i18n infrastructure):
// 1. Choice already stored in localStorage (anonymous visitor or
//    previously signed-in user whose profile language was taken over at login)
// 2. Browser language, if supported
// 3. Fallback DEFAULT_LOCALE, since the existing UI is predominantly German
export function detectInitialLocale(): string {
  const stored = localStorage.getItem(LANGUAGE_STORAGE_KEY)
  if (stored && SUPPORTED_LOCALES.includes(stored)) return stored

  const browserLang = (navigator.language || '').slice(0, 2)
  if (SUPPORTED_LOCALES.includes(browserLang)) return browserLang

  return DEFAULT_LOCALE
}

// Part 1: fallback chain of the UI texts for page languages without texts of
// their own (e.g. hr, fr): the page's default language if it has UI texts
// → English → German. Without a status page (admin area) the fallback
// stays DEFAULT_LOCALE.
export function buildFallbackChain(pageDefault: string | null | undefined): string[] {
  const chain = SUPPORTED_LOCALES.includes(pageDefault as string) ? [pageDefault as string] : []
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
