import { ref } from 'vue'
import { useApi } from './useApi'

// Globale Systemeinstellungen: Schalter für die Self-Service-
// Bestellung (öffentliche Route /order) und seit der Umstellung für den
// globalen E-Mail-Versand und für die Selbstüberwachung der Systemseite. Vorbild: useContentLanguages.js (module-scoped ref,
// nutzt apiFetch aus useApi), damit mehrere Views denselben Stand teilen.

const settings = ref({ orders_enabled: true, email_enabled: true, selfmonitor_enabled: false })

export function useSystemSettings() {
  const { apiFetch } = useApi()

  const loadSettings = async () => {
    settings.value = await apiFetch('/api/settings')
    return settings.value
  }

  // saveSettings schickt nur die übergebenen Felder (der PUT
  // speichert ausschließlich gesetzte Felder) und übernimmt den vollständigen
  // Stand aus der Antwort.
  const saveSettings = async (patch) => {
    settings.value = await apiFetch('/api/settings', {
      method: 'PUT',
      body: JSON.stringify(patch),
    })
    return settings.value
  }

  return {
    settings,
    loadSettings,
    saveSettings,
  }
}
