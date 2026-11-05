import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { SUPPORTED_LOCALES, LANGUAGE_STORAGE_KEY, loadLocaleMessages } from '../i18n'
import { applyGlobalOverrides } from '../i18n/overrides'
import { useAuth } from './useAuth'
import { useApi } from './useApi'
import { usePublicPageLanguages } from './usePublicPageLanguages'

// useLanguage kapselt die Sprachauswahl (i18n-Infrastruktur):
// - Anonyme Besucher: Auswahl landet nur im localStorage (kein Backend-Call).
// - Eingeloggte User: zusätzlich wird die Wahl per PATCH /api/me serverseitig
//   auf models.User.PreferredLanguage persistiert, damit sie geräteübergreifend
//   erhalten bleibt.
export function useLanguage() {
  const { locale, t, te } = useI18n()
  const { isLoggedIn } = useAuth()
  const { apiFetch } = useApi()
  // verständliche Meldung, wenn das serverseitige Speichern fehlschlägt.
  const saveError = ref('')
  const { pageLanguages } = usePublicPageLanguages()

  // Auf einer Statusseite bietet der Umschalter alle für die Seite
  // aktivierten Sprachen an (mit Eigenbezeichnung), sonst die Oberflächensprachen.
  // Fehlen für eine Seitensprache Oberflächentexte (nur de/en gibt es), greift
  // der Rückfall von vue-i18n; die Inhalte kommen trotzdem in dieser Sprache.
  const languageOptions = computed(() =>
    pageLanguages.value.length > 0
      ? pageLanguages.value.map((l) => ({ code: l.code, label: l.name || l.code }))
      : SUPPORTED_LOCALES.map((code) => ({ code, label: te(`language.${code}`) ? t(`language.${code}`) : code }))
  )
  // Der Wert, den der Umschalter anzeigt: die Locale, sofern angeboten, sonst
  // die Standardsprache der Seite (deren Inhalt dann auch erscheint).
  const selectedLocale = computed(() => {
    if (languageOptions.value.some((o) => o.code === locale.value)) return locale.value
    return pageLanguages.value.find((l) => l.is_default)?.code ?? locale.value
  })

  const setLanguage = async (lang) => {
    if (!languageOptions.value.some((o) => o.code === lang)) return

    // Texte einer nachladbaren Oberflächensprache vor dem Umschalten laden.
    if (SUPPORTED_LOCALES.includes(lang)) await loadLocaleMessages(lang)
    locale.value = lang
    localStorage.setItem(LANGUAGE_STORAGE_KEY, lang)
    // Seitensprachen ohne Oberflächentexte: weder Overrides (es gibt keine
    // Basistexte, die sie überlagern) noch Profil (preferred_language kennt
    // nur die Oberflächensprachen).
    if (!SUPPORTED_LOCALES.includes(lang)) {
      saveError.value = ''
      return
    }
    // Globale UI-Text-Overrides der neu gewählten Sprache nachladen - beim App-Start wird nur die initial erkannte Locale
    // geladen (siehe main.js), ein Sprachwechsel muss die Overrides der
    // anderen Sprache also separat holen.
    applyGlobalOverrides(lang)
    saveError.value = ''

    if (isLoggedIn.value) {
      try {
        await apiFetch('/api/me', {
          method: 'PATCH',
          body: JSON.stringify({ preferred_language: lang })
        })
      } catch (err) {
        // Sprachumschaltung im UI soll auch funktionieren, wenn die
        // serverseitige Persistenz gerade fehlschlägt (z.B. Netzwerkfehler) -
        // die Wahl bleibt dann zumindest lokal über localStorage erhalten.
        console.error('Konnte Sprachpräferenz nicht speichern:', err)
        saveError.value = t('language.saveError')
      }
    }
  }

  return { locale, supportedLocales: SUPPORTED_LOCALES, languageOptions, selectedLocale, setLanguage, saveError }
}
