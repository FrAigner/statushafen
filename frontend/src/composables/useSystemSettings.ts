import { ref } from 'vue'
import { useApi } from './useApi'
import type { SystemSettings } from '../types/api'

// Global system settings: switches for self-service ordering (public route
// /order) and, since the migration, for global email sending and for the
// self-monitoring of the system page. Modelled on useContentLanguages (module-scoped ref,
// uses apiFetch from useApi), so several views share the same state.

const settings = ref<SystemSettings>({ orders_enabled: true, email_enabled: true, selfmonitor_enabled: false })

export function useSystemSettings() {
  const { apiFetch } = useApi()

  const loadSettings = async () => {
    settings.value = await apiFetch<SystemSettings>('/api/settings')
    return settings.value
  }

  // saveSettings sends only the given fields (the PUT stores only fields that
  // are set) and takes the complete state from the response.
  const saveSettings = async (patch: Partial<SystemSettings>) => {
    settings.value = await apiFetch<SystemSettings>('/api/settings', {
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
