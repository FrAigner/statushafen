import { ref } from 'vue'
import i18n, { buildFallbackChain, detectInitialLocale, DEFAULT_LOCALE, LANGUAGE_STORAGE_KEY, SUPPORTED_LOCALES } from '../i18n'
import type { ContentLanguage } from '../types/api'

// vue-i18n types the locale (and fallback chain) as bundled message locales; page languages may be any code.
type I18nLocale = typeof i18n.global.locale.value

// The languages activated for the currently shown status page
// ([{ code, name, is_default }], from the dashboard). StatusPage.vue sets them,
// the language switcher in the header (PublicLayout.vue, via useLanguage) then
// offers these languages instead of only the UI languages. Shared state like
// useCurrentPageSlug, because header and page are separate components.
const pageLanguages = ref<ContentLanguage[]>([])

export function usePublicPageLanguages() {
  const setPageLanguages = (list: unknown) => {
    pageLanguages.value = Array.isArray(list)
      ? list.filter((l): l is ContentLanguage => l && typeof l.code === 'string' && l.code !== '')
      : []
    // Fallback chain of the UI texts from the page's default language.
    i18n.global.fallbackLocale.value = buildFallbackChain(pageLanguages.value.find((l) => l.is_default)?.code) as typeof i18n.global.fallbackLocale.value
    // A previously chosen page language without UI texts (e.g. hr) is unknown
    // to detectInitialLocale(); it is restored here, where the page's
    // languages are known.
    const stored = localStorage.getItem(LANGUAGE_STORAGE_KEY)
    if (
      stored &&
      stored !== i18n.global.locale.value &&
      !SUPPORTED_LOCALES.includes(stored) &&
      pageLanguages.value.some((l) => l.code === stored)
    ) {
      i18n.global.locale.value = stored as I18nLocale
    }
  }

  // When leaving the status page: clear the list and reset a language without
  // UI texts (e.g. hr) to the UI language, otherwise the admin area would keep
  // a locale the switcher does not know.
  const clearPageLanguages = () => {
    pageLanguages.value = []
    i18n.global.fallbackLocale.value = DEFAULT_LOCALE
    if (!SUPPORTED_LOCALES.includes(i18n.global.locale.value)) {
      i18n.global.locale.value = detectInitialLocale() as I18nLocale
    }
  }

  return { pageLanguages, setPageLanguages, clearPageLanguages }
}
