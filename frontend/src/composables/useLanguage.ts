import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { SUPPORTED_LOCALES, LANGUAGE_STORAGE_KEY, loadLocaleMessages } from '../i18n'
import { applyGlobalOverrides } from '../i18n/overrides'
import { useAuth } from './useAuth'
import { useApi } from './useApi'
import { usePublicPageLanguages } from './usePublicPageLanguages'

/** Entry of the language switcher. */
export interface LanguageOption {
  code: string
  label: string
}

// useLanguage encapsulates the language selection (i18n infrastructure):
// - Anonymous visitors: the choice only goes to localStorage (no backend call).
// - Logged-in users: the choice is additionally persisted on the server via
//   PATCH /api/me to models.User.PreferredLanguage, so it is kept across
//   devices.
export function useLanguage() {
  const { locale, t, te } = useI18n()
  const { isLoggedIn } = useAuth()
  const { apiFetch } = useApi()
  // understandable message when saving on the server fails.
  const saveError = ref('')
  const { pageLanguages } = usePublicPageLanguages()

  // On a status page the switcher offers all languages activated for the page
  // (with their own names), otherwise the UI languages. If a page language has
  // no UI texts (only de/en exist), vue-i18n's fallback applies; the content
  // still comes in that language.
  // With a single page language (translation switched off) there is no content
  // language to choose: the switcher offers the UI languages, the content stays in
  // the page default (the server ignores ?lang then).
  const languageOptions = computed<LanguageOption[]>(() =>
    pageLanguages.value.length > 1
      ? pageLanguages.value.map((l) => ({ code: l.code, label: l.name || l.code }))
      : SUPPORTED_LOCALES.map((code) => ({ code, label: te(`language.${code}`) ? t(`language.${code}`) : code }))
  )
  // The value the switcher shows: the locale if offered, otherwise the page's
  // default language (whose content then appears as well).
  const selectedLocale = computed<string>(() => {
    if (languageOptions.value.some((o) => o.code === locale.value)) return locale.value
    return pageLanguages.value.find((l) => l.is_default)?.code ?? locale.value
  })

  const setLanguage = async (lang: string): Promise<void> => {
    if (!languageOptions.value.some((o) => o.code === lang)) return

    // Load the texts of a lazily loadable UI language before switching.
    if (SUPPORTED_LOCALES.includes(lang)) await loadLocaleMessages(lang)
    locale.value = lang
    localStorage.setItem(LANGUAGE_STORAGE_KEY, lang)
    // Page languages without UI texts: neither overrides (there are no base
    // texts for them to overlay) nor profile (preferred_language only knows
    // the UI languages).
    if (!SUPPORTED_LOCALES.includes(lang)) {
      saveError.value = ''
      return
    }
    // Load the global UI text overrides of the newly chosen language: app start only loads the initially detected locale
    // (see main.js), so a language switch has to fetch the overrides of the
    // other language separately.
    applyGlobalOverrides(lang)
    saveError.value = ''

    if (isLoggedIn.value) {
      try {
        await apiFetch('/api/me', {
          method: 'PATCH',
          body: JSON.stringify({ preferred_language: lang })
        })
      } catch (err) {
        // Switching the UI language should work even when persisting on the
        // server currently fails (e.g. network error); the choice is then at
        // least kept locally via localStorage.
        console.error('Konnte Sprachpräferenz nicht speichern:', err)
        saveError.value = t('language.saveError')
      }
    }
  }

  return { locale, supportedLocales: SUPPORTED_LOCALES, languageOptions, selectedLocale, setLanguage, saveError }
}
