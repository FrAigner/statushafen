import { ref } from 'vue'
import { useApi } from './useApi'
import type { TranslationSettings } from '../types/api'

// Settings of the automatic translation (system settings).
// The key never comes back from the server, only api_key_set.
const settings = ref<TranslationSettings>({
  enabled: false, provider: '', url: '', api_key_set: false, env_url_set: false, env_api_key_set: false,
  effective_provider: '', configured: false,
})

/** Response of POST /api/settings/translation/test. */
export interface TranslationTestResult {
  ok: boolean
  provider: string
  sample: string
}

export function useTranslationSettings() {
  const { apiFetch } = useApi()

  const load = async () => {
    settings.value = await apiFetch<TranslationSettings>('/api/settings/translation')
    return settings.value
  }

  // patch: only changed fields (enabled, provider, url, api_key); api_key '' deletes the key.
  const save = async (patch: Partial<Pick<TranslationSettings, 'enabled' | 'provider' | 'url'>> & { api_key?: string }) => {
    settings.value = await apiFetch<TranslationSettings>('/api/settings/translation', { method: 'PUT', body: JSON.stringify(patch) })
    return settings.value
  }

  // Connection test with the stored configuration.
  const test = () => apiFetch<TranslationTestResult>('/api/settings/translation/test', { method: 'POST' })

  return { settings, load, save, test }
}
