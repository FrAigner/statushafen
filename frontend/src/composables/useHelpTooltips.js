import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuth } from './useAuth'
import { useApi } from './useApi'

// Hilfe-Tooltips im Admin-Bereich pro Benutzer ein- und
// ausschalten. Gespeichert wird serverseitig am User (PATCH /api/me
// {"help_tooltips": bool}, Ausgabe in /api/me, Login-Antwort und SSO-Token),
// damit die Wahl auf allen Geräten gilt. Default ist an: Fehlt das Feld
// (alter Client-Zustand, Test-Setups), gelten die Tooltips als eingeschaltet.
//
// Der Schalter wirkt sofort über currentUser (lokaler Zustand), die Antwort
// des Servers wird nicht abgewartet. Schlägt das Speichern fehl, bleibt die
// Wahl für diese Sitzung erhalten und saveError nennt das Problem (wie bei
// der Sprachwahl, useLanguage.js).

let auth = null
const user = () => (auth ??= useAuth()).currentUser

export const helpTooltipsEnabled = computed(() => user().value?.help_tooltips !== false)

export function useHelpTooltips() {
  const { t } = useI18n()
  const { apiFetch } = useApi()
  const saveError = ref('')

  const setHelpTooltips = async (on) => {
    const current = user()
    if (!current.value) return
    current.value = { ...current.value, help_tooltips: on }
    saveError.value = ''
    try {
      await apiFetch('/api/me', { method: 'PATCH', body: JSON.stringify({ help_tooltips: on }) })
    } catch (err) {
      console.error('Konnte Hilfe-Einstellung nicht speichern:', err)
      saveError.value = t('helpTooltips.saveError')
    }
  }

  return { enabled: helpTooltipsEnabled, setHelpTooltips, saveError }
}
