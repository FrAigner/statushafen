import { ref } from 'vue'
import i18n, { buildFallbackChain, detectInitialLocale, DEFAULT_LOCALE, LANGUAGE_STORAGE_KEY, SUPPORTED_LOCALES } from '../i18n'

// Die für die gerade angezeigte Statusseite aktivierten Sprachen
// ([{ code, name, is_default }], aus dem Dashboard). StatusPage.vue setzt sie,
// der Sprachumschalter im Kopf (PublicLayout.vue, über useLanguage) bietet dann
// diese Sprachen an statt nur der Oberflächensprachen. Geteilter Zustand wie
// useCurrentPageSlug, weil Kopf und Seite getrennte Komponenten sind.
const pageLanguages = ref([])

export function usePublicPageLanguages() {
  const setPageLanguages = (list) => {
    pageLanguages.value = Array.isArray(list)
      ? list.filter((l) => l && typeof l.code === 'string' && l.code !== '')
      : []
    // Rückfallkette der Oberflächentexte aus der Standardsprache der Seite.
    i18n.global.fallbackLocale.value = buildFallbackChain(pageLanguages.value.find((l) => l.is_default)?.code)
    // Eine früher gewählte Seitensprache ohne Oberflächentexte (z. B. hr) kennt
    // detectInitialLocale() nicht; hier, wo die Sprachen der Seite bekannt sind,
    // wird sie wiederhergestellt.
    const stored = localStorage.getItem(LANGUAGE_STORAGE_KEY)
    if (
      stored &&
      stored !== i18n.global.locale.value &&
      !SUPPORTED_LOCALES.includes(stored) &&
      pageLanguages.value.some((l) => l.code === stored)
    ) {
      i18n.global.locale.value = stored
    }
  }

  // Beim Verlassen der Statusseite: Liste leeren und eine Sprache ohne
  // Oberflächentexte (z. B. hr) auf die Oberflächensprache zurücksetzen, sonst
  // bliebe im Admin-Bereich eine Locale stehen, die der Umschalter nicht kennt.
  const clearPageLanguages = () => {
    pageLanguages.value = []
    i18n.global.fallbackLocale.value = DEFAULT_LOCALE
    if (!SUPPORTED_LOCALES.includes(i18n.global.locale.value)) {
      i18n.global.locale.value = detectInitialLocale()
    }
  }

  return { pageLanguages, setPageLanguages, clearPageLanguages }
}
