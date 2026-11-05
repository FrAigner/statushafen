import { ref } from 'vue'
import { useApi } from './useApi'

// Einstellungen der automatischen Übersetzung (Systemeinstellungen).
// Der Schlüssel kommt nie vom Server zurück, nur api_key_set.
const settings = ref({
  provider: '', url: '', api_key_set: false, env_url_set: false, env_api_key_set: false,
  effective_provider: '', configured: false,
})

export function useTranslationSettings() {
  const { apiFetch } = useApi()

  const load = async () => {
    settings.value = await apiFetch('/api/settings/translation')
    return settings.value
  }

  // patch: nur geänderte Felder (provider, url, api_key); api_key '' löscht den Schlüssel.
  const save = async (patch) => {
    settings.value = await apiFetch('/api/settings/translation', { method: 'PUT', body: JSON.stringify(patch) })
    return settings.value
  }

  // Verbindungstest mit der gespeicherten Konfiguration.
  const test = () => apiFetch('/api/settings/translation/test', { method: 'POST' })

  return { settings, load, save, test }
}
