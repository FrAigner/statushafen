import { ref } from 'vue'
import { useApi } from './useApi'

// Switch "Automatic translation" of the system settings (default off). Only while
// it is on do incidents, maintenance, notices, components, templates and mails have
// a second content language (de/en) next to the page's default language. The admin
// views read it from the public flag endpoint, because GET /api/settings/translation
// is reserved for global admins. Module-scoped like useSystemSettings, so saving the
// switch in TranslationSettings is reflected in every open view.
const enabled = ref(false)

export function useTranslationEnabled() {
  const { apiFetch } = useApi()

  // A failed request counts as "off", the default of the server.
  const load = async () => {
    try {
      const res = await apiFetch<{ enabled?: boolean } | null>('/api/public/translation/enabled')
      enabled.value = res?.enabled === true
    } catch {
      enabled.value = false
    }
    return enabled.value
  }

  return { enabled, load }
}
